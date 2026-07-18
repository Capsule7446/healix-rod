package rod

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"

	"github.com/Capsule7446/healix-core/domain/fingerprint"
	"github.com/Capsule7446/healix-core/domain/node"
)

const testHTML = `<!DOCTYPE html>
<html><body>
<form id="loginForm">
  <label for="username">Username</label>
  <input id="username" type="text">
  <button id="submit" type="submit" data-testid="login-submit">登录</button>
</form>
<div id="result"></div>
<input id="enter-target" type="text">
<div id="enter-result"></div>
<div id="assert-target"><span>普通断言目标</span></div>
<span id="label-prefix">Account</span><span id="label-suffix">email</span>
<input id="email" type="email" aria-labelledby="label-prefix label-suffix">
<script>
document.getElementById('submit').addEventListener('click', function (e) {
  e.preventDefault();
  document.getElementById('result').innerText = '欢迎, ' + document.getElementById('username').value;
});
document.getElementById('enter-target').addEventListener('keydown', function (e) {
  if (e.key === 'Enter') document.getElementById('enter-result').innerText = 'submitted';
});
</script>
</body></html>`

func writeTestHTML(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "login.html")
	if err := os.WriteFile(p, []byte(testHTML), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}
	return "file://" + p
}

func TestCleanupFailedLaunchRemovesProfileWithoutBlocking(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	launch := launcher.New().UserDataDir(profile)
	cleanupFailedLaunch(launch)
	if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile still exists or stat failed: %v", err)
	}
}

func TestDriver_NavigateLocateActAssert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := writeTestHTML(t)
	if err := d.Navigate(ctx, url); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	usernameSpec := fingerprint.NodeSpec{
		ID:        "login.username",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#username", Priority: 0}},
	}
	el, err := d.Locate(ctx, usernameSpec)
	if err != nil {
		t.Fatalf("Locate username: %v", err)
	}
	if err := el.Input(ctx, "alice"); err != nil {
		t.Fatalf("Input: %v", err)
	}

	submitSpec := fingerprint.NodeSpec{
		ID: "login.submit",
		Selectors: []fingerprint.Selector{
			{Type: fingerprint.SelectorTestID, Value: "login-submit", Priority: 0},
			{Type: fingerprint.SelectorCSS, Value: "#submit", Priority: 1},
		},
	}
	submitEl, err := d.Locate(ctx, submitSpec)
	if err != nil {
		t.Fatalf("Locate submit: %v", err)
	}
	if err := submitEl.WaitStable(ctx); err != nil {
		t.Fatalf("WaitStable: %v", err)
	}
	if err := submitEl.Click(ctx); err != nil {
		t.Fatalf("Click: %v", err)
	}

	resultSpec := fingerprint.NodeSpec{
		ID:        "login.result",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#result", Priority: 0}},
	}
	resultEl, err := d.Locate(ctx, resultSpec)
	if err != nil {
		t.Fatalf("Locate result: %v", err)
	}
	text, err := resultEl.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if want := "欢迎, alice"; text != want {
		t.Fatalf("result text = %q, want %q", text, want)
	}

	enterTarget, err := d.Locate(ctx, fingerprint.NodeSpec{
		ID:        "enter.target",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#enter-target", Priority: 0}},
	})
	if err != nil {
		t.Fatalf("Locate Enter target: %v", err)
	}
	if err := enterTarget.Input(ctx, "query"); err != nil {
		t.Fatalf("Input before Enter: %v", err)
	}
	if err := d.Press(ctx, "Enter"); err != nil {
		t.Fatalf("Press Enter: %v", err)
	}
	enterResult, err := d.Locate(ctx, fingerprint.NodeSpec{
		ID:        "enter.result",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#enter-result", Priority: 0}},
	})
	if err != nil {
		t.Fatalf("Locate Enter result: %v", err)
	}
	enterText, err := enterResult.Text(ctx)
	if err != nil {
		t.Fatalf("Enter result text: %v", err)
	}
	if enterText != "submitted" {
		t.Fatalf("Enter result text = %q, want submitted", enterText)
	}
}

func TestElement_SelectSupportsAntDesignSelect(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ant-select.html")
	if err := os.WriteFile(p, []byte(`<!doctype html>
<html><head><style>.ant-select-dropdown-hidden{display:none}</style></head><body>
<div id="audience-select" class="ant-select">
  <div class="ant-select-selector">
    <span class="ant-select-selection-placeholder">请选择</span>
    <input role="combobox" aria-label="请选择">
  </div>
</div>
<div id="audience-dropdown" class="ant-select-dropdown ant-select-dropdown-hidden">
  <div class="ant-select-item-option"><div class="ant-select-item-option-content">绑定未验证</div></div>
  <div class="ant-select-item-option"><div class="ant-select-item-option-content">已验证</div></div>
</div>
<div id="result"></div>
<script>
const root = document.querySelector('#audience-select');
const dropdown = document.querySelector('#audience-dropdown');
root.addEventListener('mousedown', () => {
  root.classList.add('ant-select-open');
  dropdown.classList.remove('ant-select-dropdown-hidden');
});
dropdown.addEventListener('click', (event) => {
  const option = event.target.closest('.ant-select-item-option');
  if (!option) return;
  const result = document.querySelector('#result');
  result.textContent = [result.textContent, option.textContent.trim()].filter(Boolean).join(',');
});
	document.addEventListener('keydown', (event) => {
	  if (event.key !== 'Escape') return;
	  root.classList.remove('ant-select-open');
	  dropdown.classList.add('ant-select-dropdown-hidden');
	});
</script>
</body></html>`), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}

	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, "file://"+p); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	el, err := d.Locate(ctx, fingerprint.NodeSpec{
		ID:        "audience.select",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#audience-select", Priority: 0}},
	})
	if err != nil {
		t.Fatalf("Locate select: %v", err)
	}
	if err := el.Select(ctx, "绑定未验证", "已验证"); err != nil {
		t.Fatalf("Select: %v", err)
	}

	result, err := d.Locate(ctx, fingerprint.NodeSpec{
		ID:        "result",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#result", Priority: 0}},
	})
	if err != nil {
		t.Fatalf("Locate result: %v", err)
	}
	text, err := result.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if text != "绑定未验证,已验证" {
		t.Fatalf("selected text = %q", text)
	}
	dropdown, err := d.Locate(ctx, fingerprint.NodeSpec{
		ID:        "dropdown",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#audience-dropdown", Priority: 0}},
	})
	if err != nil {
		t.Fatalf("Locate dropdown: %v", err)
	}
	className, ok, err := dropdown.Attribute(ctx, "class")
	if err != nil {
		t.Fatalf("dropdown class: %v", err)
	}
	if !ok || className != "ant-select-dropdown" {
		t.Fatalf("dropdown class after select = %q, want open until explicit Escape", className)
	}
	if err := d.Press(ctx, "Escape"); err != nil {
		t.Fatalf("Press Escape: %v", err)
	}
	className, ok, err = dropdown.Attribute(ctx, "class")
	if err != nil {
		t.Fatalf("dropdown class after Escape: %v", err)
	}
	if !ok || className != "ant-select-dropdown ant-select-dropdown-hidden" {
		t.Fatalf("dropdown class after Escape = %q, want hidden", className)
	}
}

func TestDriver_LocateAllSelectorsFailReturnsErrNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true, LocateTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	spec := fingerprint.NodeSpec{
		ID:        "ghost",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#does-not-exist", Priority: 0}},
	}
	if _, err := d.Locate(ctx, spec); !errors.Is(err, node.ErrElementNotFound) {
		t.Fatalf("error = %v, want ErrElementNotFound", err)
	}
}

func TestElement_WaitStableAllowsVisibleLiveElementAfterLocalTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "live.html")
	if err := os.WriteFile(path, []byte(`<!doctype html>
<button id="live" onclick="this.textContent='clicked'">click me</button>
<script>
let wide = false;
window.liveResizeTimer = setInterval(() => {
  wide = !wide;
  document.getElementById('live').style.width = wide ? '120px' : '240px';
}, 50);
</script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, "file://"+path); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	el, err := d.Locate(ctx, fingerprint.NodeSpec{
		ID:        "live",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#live"}},
	})
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if err := el.WaitStable(ctx); err != nil {
		t.Fatalf("WaitStable: %v", err)
	}
	// WaitStable's fallback contract only says a continuously moving but live
	// element may proceed after the adapter-local timeout. Stop this fixture's
	// synthetic movement before asserting the separate pointer-click contract.
	if _, err := d.RawPage().Context(ctx).Eval(`() => clearInterval(window.liveResizeTimer)`); err != nil {
		t.Fatalf("stop live resize: %v", err)
	}
	if err := el.Click(ctx); err != nil {
		t.Fatalf("Click: %v", err)
	}
	text, err := el.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if text != "clicked" {
		t.Fatalf("text = %q, want clicked", text)
	}
}

func TestElement_ClickFallsBackToPointerEnabledAncestor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pointer.html")
	if err := os.WriteFile(path, []byte(`<!doctype html>
<button id="parent" onclick="this.textContent='clicked'">
  <span id="inner" style="pointer-events: none">inner</span>
</button>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, "file://"+path); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	el, err := d.Locate(ctx, fingerprint.NodeSpec{
		ID:        "inner",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#inner"}},
	})
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if err := el.Click(ctx); err != nil {
		t.Fatalf("Click: %v", err)
	}
	parent, err := d.Locate(ctx, fingerprint.NodeSpec{
		ID:        "parent",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#parent"}},
	})
	if err != nil {
		t.Fatalf("Locate parent: %v", err)
	}
	text, err := parent.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if text != "clicked" {
		t.Fatalf("text = %q, want clicked", text)
	}
}

func TestDriver_LocateDoesNotClassifyInvalidSelectorAsNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true, LocateTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	_, err = d.Locate(ctx, fingerprint.NodeSpec{ID: "invalid", Selectors: []fingerprint.Selector{
		{Type: fingerprint.SelectorCSS, Value: "[", Priority: 0},
	}})
	if err == nil || errors.Is(err, node.ErrElementNotFound) {
		t.Fatalf("error = %v, want non-not-found selector error", err)
	}
}

func TestDriver_LocateEmptySelectorsIsNotNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	_, err = d.Locate(context.Background(), fingerprint.NodeSpec{ID: "invalid"})
	if err == nil || errors.Is(err, node.ErrElementNotFound) {
		t.Fatalf("error = %v, want non-not-found spec error", err)
	}
}

func TestDriver_LocateDoesNotClassifyClosedBrowserAsNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true, LocateTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = d.Locate(context.Background(), fingerprint.NodeSpec{ID: "browser-closed", Selectors: []fingerprint.Selector{
		{Type: fingerprint.SelectorCSS, Value: "#submit", Priority: 0},
	}})
	if err == nil || errors.Is(err, node.ErrElementNotFound) {
		t.Fatalf("error = %v, want non-not-found browser error", err)
	}
}

func TestDriver_LocatePreservesCallerCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true, LocateTimeout: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		cancel()
		t.Fatalf("Navigate: %v", err)
	}
	cancel()

	_, err = d.Locate(ctx, fingerprint.NodeSpec{ID: "cancelled", Selectors: []fingerprint.Selector{
		{Type: fingerprint.SelectorCSS, Value: "#does-not-exist", Priority: 0},
	}})
	if !errors.Is(err, context.Canceled) || errors.Is(err, node.ErrElementNotFound) {
		t.Fatalf("error = %v, want context.Canceled only", err)
	}
}

func TestElement_ExistsDistinguishesDetachAndCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	el, err := d.Locate(ctx, fingerprint.NodeSpec{ID: "result", Selectors: []fingerprint.Selector{
		{Type: fingerprint.SelectorCSS, Value: "#result", Priority: 0},
	}})
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}

	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := el.Exists(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Exists cancellation error = %v, want context.Canceled", err)
	}
	if _, err := d.page.Context(ctx).Eval(`() => document.querySelector('#result').remove()`); err != nil {
		t.Fatalf("remove element: %v", err)
	}
	exists, err := el.Exists(ctx)
	if err != nil || exists {
		t.Fatalf("Exists after detach = %v, %v; want false, nil", exists, err)
	}
}

func TestDriver_LocateRoleUsesImplicitRoleAndAccessibleName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	el, err := d.Locate(ctx, fingerprint.NodeSpec{ID: "email", Selectors: []fingerprint.Selector{
		{Type: fingerprint.SelectorRole, Value: `textbox[name="Account email"]`, Priority: 0},
	}})
	if err != nil {
		t.Fatalf("Locate role: %v", err)
	}
	id, ok, err := el.Attribute(ctx, "id")
	if err != nil || !ok || id != "email" {
		t.Fatalf("located id = %q, ok=%v, err=%v", id, ok, err)
	}
}

func TestDriver_LocateInvalidRoleSyntaxIsNotNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	_, err = d.Locate(ctx, fingerprint.NodeSpec{ID: "invalid-role", Selectors: []fingerprint.Selector{
		{Type: fingerprint.SelectorRole, Value: "button[name=]", Priority: 0},
	}})
	if err == nil || errors.Is(err, node.ErrElementNotFound) {
		t.Fatalf("error = %v, want non-not-found role syntax error", err)
	}
}

func TestDriver_SnapshotFindsCandidates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	snap, err := d.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	candidates, err := snap.Candidates(ctx)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}

	found := false
	for _, c := range candidates {
		if c.Fingerprint.Attributes["data-testid"] == "login-submit" {
			found = true
			if c.Fingerprint.Tag != "button" {
				t.Fatalf("expected tag=button, got %q", c.Fingerprint.Tag)
			}
			if c.Fingerprint.FormID != "loginForm" {
				t.Fatalf("expected submit button FormID=loginForm, got %q", c.Fingerprint.FormID)
			}
		}
	}
	if !found {
		t.Fatalf("expected to find the login-submit button among candidates, got %d candidates", len(candidates))
	}
}

func TestDriver_SnapshotCapturesLabelTextAndFormID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	snap, err := d.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	candidates, err := snap.Candidates(ctx)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}

	found := false
	for _, c := range candidates {
		if c.Fingerprint.Attributes["id"] != "username" {
			continue
		}
		found = true
		if c.Fingerprint.LabelText != "Username" {
			t.Fatalf("expected LabelText=Username (from <label for=username>), got %q", c.Fingerprint.LabelText)
		}
		if c.Fingerprint.FormID != "loginForm" {
			t.Fatalf("expected FormID=loginForm, got %q", c.Fingerprint.FormID)
		}
	}
	if !found {
		t.Fatalf("expected to find the #username input among candidates, got %d candidates", len(candidates))
	}
}

func TestDriver_SnapshotIncludesOrdinaryAssertionTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	snap, err := d.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	candidates, err := snap.Candidates(ctx)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.Fingerprint.Attributes["id"] == "assert-target" {
			return
		}
	}
	t.Fatalf("ordinary div assertion target was absent from %d candidates", len(candidates))
}

func TestDriver_SnapshotCapturesImplicitARIA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.Navigate(ctx, writeTestHTML(t)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	snap, err := d.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	candidates, err := snap.Candidates(ctx)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.Fingerprint.Attributes["id"] != "email" {
			continue
		}
		if candidate.Fingerprint.ARIA.Role != "textbox" || candidate.Fingerprint.ARIA.Name != "Account email" {
			t.Fatalf("ARIA = %+v, want textbox/Account email", candidate.Fingerprint.ARIA)
		}
		return
	}
	t.Fatalf("email candidate was absent from %d candidates", len(candidates))
}
