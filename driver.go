// Package rod 基于 go-rod 实现浏览器自动化的 Driver 端口（domain/node）
// （方案 §10）。选用基于 remote-object-id 的 go-rod，而非 chromedp 的
// DOM-node-id 方案，正是为了规避方案 Key Finding 3 中记录的本地
// NodeID/DOM.documentUpdated 竞态问题。
package rod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"

	"github.com/Capsule7446/healix-core/domain/fingerprint"
	"github.com/Capsule7446/healix-core/domain/heal"
	"github.com/Capsule7446/healix-core/domain/node"
)

// errSelectorNotFound is internal to the adapter. Locate translates it and
// go-rod's local locate timeout to the domain's explicit not-found contract.
var errSelectorNotFound = errors.New("rod: selector matched no element")

// Options 用于配置 New。
type Options struct {
	Headless bool
	// BrowserPath optionally selects a user-installed Chromium/Chrome binary.
	// Empty keeps go-rod's managed Chromium behavior.
	BrowserPath string
	// LocateTimeout 限定单次 selector 尝试的最长耗时，
	// 超时后会继续尝试下一个 selector。
	LocateTimeout time.Duration
}

const navigationTimeout = 30 * time.Second

// Driver 是 node.Driver 的 go-rod 实现。一个 Driver 拥有一个
// browser + 一个 page。
type Driver struct {
	browser *rod.Browser
	page    *rod.Page
	launch  *launcher.Launcher
	timeout time.Duration
}

var _ node.Driver = (*Driver)(nil)

// New 启动一个固定版本的 Chromium（方案 §10.4）并打开一个页面。
func New(opts Options) (*Driver, error) {
	l := launcher.New().Headless(opts.Headless).Revision(launcher.RevisionDefault)
	if opts.BrowserPath != "" {
		l.Bin(opts.BrowserPath)
	}
	if !opts.Headless {
		l.Set("start-maximized")
	}
	controlURL, err := l.Launch()
	if err != nil {
		cleanupFailedLaunch(l)
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(controlURL).NoDefaultDevice()
	if err := browser.Connect(); err != nil {
		_ = cleanupLaunchedBrowser(browser, l)
		return nil, fmt.Errorf("connect browser: %w", err)
	}

	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		_ = cleanupLaunchedBrowser(browser, l)
		return nil, fmt.Errorf("open page: %w", err)
	}

	timeout := opts.LocateTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return &Driver{browser: browser, page: page, launch: l, timeout: timeout}, nil
}

// cleanupFailedLaunch cannot use launcher.Cleanup because go-rod only closes
// its internal exit channel after a process starts successfully.
func cleanupFailedLaunch(l *launcher.Launcher) {
	if l.PID() != 0 {
		l.Kill()
	}
	_ = os.RemoveAll(l.Get(flags.UserDataDir))
}

func cleanupLaunchedBrowser(browser *rod.Browser, l *launcher.Launcher) error {
	err := browser.Close()
	if err != nil {
		l.Kill()
	}
	l.Cleanup()
	return err
}

// Close 关闭该 page 所属的 browser，并清理 launcher 的临时
// profile 目录。
func (d *Driver) Close() error {
	return cleanupLaunchedBrowser(d.browser, d.launch)
}

// Wait blocks until the controlled page is closed or ctx is cancelled.
func (d *Driver) Wait(ctx context.Context) error {
	select {
	case <-d.page.GetContext().Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Driver) Closed() bool {
	select {
	case <-d.page.GetContext().Done():
		return true
	default:
		return false
	}
}

func (d *Driver) Done() <-chan struct{} { return d.page.GetContext().Done() }

// RawPage exposes the run-scoped CDP page to infrastructure collaborators.
// It must never cross into application or domain APIs.
func (d *Driver) RawPage() *rod.Page { return d.page }

// CaptureViewportJPEG captures the browser's current viewport without
// scrolling, resizing, or changing page state. It is deliberately an
// infrastructure-only capability rather than part of node.Driver.
func (d *Driver) CaptureViewportJPEG(ctx context.Context, quality int) ([]byte, error) {
	if d == nil || d.page == nil {
		return nil, errors.New("browser page is unavailable")
	}
	if quality < 0 || quality > 100 {
		return nil, fmt.Errorf("JPEG quality %d must be between 0 and 100", quality)
	}
	return d.page.Context(ctx).Screenshot(false, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatJpeg, Quality: gson.Int(quality),
	})
}

// Navigate 加载 url 并等待 DOM 稳定（方案 §10.3）。
func (d *Driver) Navigate(ctx context.Context, url string) error {
	p := d.page.Context(ctx)
	if err := p.Navigate(url); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("browser navigation failed")
	}
	if err := p.Timeout(navigationTimeout).WaitDOMStable(300*time.Millisecond, 0); err != nil {
		return fmt.Errorf("wait DOM stable after navigate: %w", err)
	}
	return nil
}

func (d *Driver) Press(ctx context.Context, key string) error {
	switch key {
	case "Escape", "Esc":
		return d.page.Context(ctx).Keyboard.Type(input.Escape)
	case "Enter":
		return d.page.Context(ctx).Keyboard.Type(input.Enter)
	default:
		return fmt.Errorf("unsupported key %q", key)
	}
}

// Open navigates without the execution engine's DOM-stability wait. Interactive
// sampling must remain usable on pages with continuous animation or mutation.
func (d *Driver) Open(ctx context.Context, url string) error {
	if err := d.page.Context(ctx).Navigate(url); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("open sampling page failed")
	}
	return nil
}

// Locate 按 priority 升序依次尝试 spec.Selectors（方案 §10.2），
// 只有当所有 selector 都明确找不到元素后才返回 ErrElementNotFound——
// 这是引擎用来触发自愈的信号。
func (d *Driver) Locate(ctx context.Context, spec fingerprint.NodeSpec) (node.Element, error) {
	sorted := sortedSelectors(spec.Selectors)
	if len(sorted) == 0 {
		return nil, fmt.Errorf("node %q has no selectors", spec.ID)
	}

	for _, sel := range sorted {
		el, err := d.locateOne(ctx, sel)
		if err == nil && el != nil {
			return &Element{el: el}, nil
		}
		if !isElementNotFound(err, ctx) {
			return nil, fmt.Errorf("locate node %q with %s selector %q: %w", spec.ID, sel.Type, sel.Value, err)
		}
	}
	return nil, fmt.Errorf("%w: node %q", node.ErrElementNotFound, spec.ID)
}

func isElementNotFound(err error, callerCtx context.Context) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errSelectorNotFound) || errors.Is(err, &rod.ElementNotFoundError{}) {
		return true
	}
	// Page.Timeout uses context.DeadlineExceeded when its private locate timeout
	// expires. A deadline/cancellation from the caller remains a system error.
	return errors.Is(err, context.DeadlineExceeded) && callerCtx.Err() == nil
}

func (d *Driver) locateOne(ctx context.Context, sel fingerprint.Selector) (*rod.Element, error) {
	p := d.page.Context(ctx).Timeout(d.timeout)
	switch sel.Type {
	case fingerprint.SelectorCSS:
		return p.Element(sel.Value)
	case fingerprint.SelectorXPath:
		return p.ElementX(sel.Value)
	case fingerprint.SelectorText:
		return locateByText(p, sel.Value)
	case fingerprint.SelectorTestID:
		return p.Element(fmt.Sprintf(`[data-testid=%q]`, sel.Value))
	case fingerprint.SelectorRole:
		return locateByRole(p, sel.Value)
	default:
		return nil, fmt.Errorf("unsupported selector type %q", sel.Type)
	}
}

func sortedSelectors(sels []fingerprint.Selector) []fingerprint.Selector {
	out := append([]fingerprint.Selector(nil), sels...)
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}

// WaitNetworkIdle 阻塞到页面持续 300ms 没有网络请求；超时由 ctx 控制
// （domain 的 WaitNode 会带条件超时的 ctx 进来）。
func (d *Driver) WaitNetworkIdle(ctx context.Context) error {
	p := d.page.Context(ctx)
	wait := p.WaitRequestIdle(300*time.Millisecond, nil, nil, nil)
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Driver) Expose(name string, fn func(gson.JSON) (interface{}, error)) (func() error, error) {
	return d.page.Expose(name, fn)
}

func (d *Driver) EvalOnNewDocument(js string) (func() error, error) {
	return d.page.EvalOnNewDocument(js)
}

func (d *Driver) EvalScript(ctx context.Context, js string) error {
	res, err := proto.RuntimeEvaluate{
		Expression:                  js,
		AwaitPromise:                true,
		ReturnByValue:               true,
		AllowUnsafeEvalBlockedByCSP: true,
	}.Call(d.page.Context(ctx))
	if err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		return fmt.Errorf("eval script exception: %s", res.ExceptionDetails.Text)
	}
	return nil
}

// Snapshot 返回供 Healer 对候选打分所用的 DOMSnapshot。
func (d *Driver) Snapshot(ctx context.Context) (heal.DOMSnapshot, error) {
	return &Snapshot{page: d.page.Context(ctx)}, nil
}
