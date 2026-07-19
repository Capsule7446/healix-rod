// Package rod implements the browser automation ports with go-rod.
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

// Options 配置浏览器启动方式和元素定位超时。
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

// Driver 实现 Core 的浏览器驱动端口，并管理一个浏览器页面。
type Driver struct {
	browser *rod.Browser
	page    *rod.Page
	launch  *launcher.Launcher
	timeout time.Duration
}

var _ node.Driver = (*Driver)(nil)

// New 启动 Chromium 并打开一个页面。
func New(opts Options) (*Driver, error) {
	l := launcher.New().Headless(opts.Headless).Revision(launcher.RevisionDefault)
	if os.Getenv("HEALIX_ROD_NO_SANDBOX") == "1" {
		l.Set("no-sandbox")
	}
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

// cleanupFailedLaunch removes launcher resources when the process does not start.
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

// Close 关闭浏览器并清理启动器创建的临时配置目录。
func (d *Driver) Close() error {
	return cleanupLaunchedBrowser(d.browser, d.launch)
}

// Wait 等待页面关闭或 ctx 被取消。
func (d *Driver) Wait(ctx context.Context) error {
	select {
	case <-d.page.GetContext().Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Closed 报告浏览器页面是否已经关闭。
func (d *Driver) Closed() bool {
	select {
	case <-d.page.GetContext().Done():
		return true
	default:
		return false
	}
}

// Done 返回一个在浏览器页面关闭时结束的通道。
func (d *Driver) Done() <-chan struct{} { return d.page.GetContext().Done() }

// RawPage 返回当前页面的 go-rod 实例，供基础设施协作者使用。
// 返回的页面对象不得进入应用层或领域层 API。
func (d *Driver) RawPage() *rod.Page { return d.page }

// CaptureViewportJPEG 在不改变页面状态的情况下截取当前视口。
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

// Navigate 加载 URL 并等待 DOM 稳定。
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

// Press 向当前页面发送受支持的键盘按键。
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

// Open 导航到 URL，但不等待 DOM 稳定，适合持续动画或持续变更的页面。
func (d *Driver) Open(ctx context.Context, url string) error {
	if err := d.page.Context(ctx).Navigate(url); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("open sampling page failed")
	}
	return nil
}

// Locate 按优先级升序尝试 selector；仅当所有 selector 都未找到元素时返回 ErrElementNotFound。
func (d *Driver) Locate(ctx context.Context, spec fingerprint.NodeSpec) (node.Element, error) {
	sorted := sortedSelectors(spec.Selectors)
	if len(sorted) == 0 {
		return nil, fmt.Errorf("node %q has no selectors", spec.ID)
	}

	locateCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	for _, sel := range sorted {
		el, err := d.locateOne(locateCtx, sel)
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

// WaitNetworkIdle 等待页面连续 300 毫秒没有网络请求。
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

// Expose 将 Go 回调注册为页面可调用的绑定，并返回移除绑定的函数。
func (d *Driver) Expose(name string, fn func(gson.JSON) (interface{}, error)) (func() error, error) {
	return d.page.Expose(name, fn)
}

// EvalOnNewDocument 将脚本注册为每个新文档的初始化脚本。
func (d *Driver) EvalOnNewDocument(js string) (func() error, error) {
	return d.page.EvalOnNewDocument(js)
}

// EvalScript 在当前页面执行 JavaScript，并返回脚本异常或传输错误。
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

// Snapshot 返回供自愈候选节点评分使用的 DOM 快照。
func (d *Driver) Snapshot(ctx context.Context) (heal.DOMSnapshot, error) {
	return &Snapshot{page: d.page.Context(ctx)}, nil
}
