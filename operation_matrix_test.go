package rod

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ysmood/gson"

	"github.com/Capsule7446/healix-core/domain/fingerprint"
	"github.com/Capsule7446/healix-core/domain/node"
)

const operationMatrixHTML = `<!doctype html>
<html><body>
  <label for="prefilled">Visible label</label>
  <input id="prefilled" value="seed" aria-label="ARIA override">
  <div id="hidden" style="display:none">hidden</div>
  <button id="hover" onmouseenter="this.dataset.hovered = 'yes'">hover</button>
  <div id="text-wrapper"><button id="text-target">Exact target</button></div>
  <button id="priority-high">high</button>
  <button id="priority-low">low</button>
  <select id="single">
    <option value="long">United States</option>
    <option value="exact">United</option>
  </select>
  <select id="multiple" multiple>
    <option value="a" selected>Alpha</option>
    <option value="b">Beta</option>
    <option value="c">Gamma</option>
  </select>
  <input id="checked" type="checkbox" checked>
  <div id="mixed" role="checkbox" aria-checked="mixed" aria-disabled="true"></div>
  <button id="pressed" aria-pressed="true">pressed</button>
  <div id="editable" contenteditable="true">editable value</div>
  <div id="ant" class="ant-select ant-select-disabled">
    <span class="ant-select-selection-item" title="north">North</span>
    <span class="ant-select-selection-item" data-value="south">South</span>
  </div>
  <section><div><div><div><div><div><div><div><div><div><button data-snapshot-marker="deep-first">deep</button></div></div></div></div></div></div></div></div></div></section>
  <section><div><div><div><div><div><div><div><div><div><button data-snapshot-marker="deep-second">deep</button></div></div></div></div></div></div></div></div></div></section>
  <div><button id="duplicate-id" data-snapshot-marker="duplicate-first">duplicate</button></div>
  <div><button id="duplicate-id" data-snapshot-marker="duplicate-second">duplicate</button></div>
</body></html>`

func writeOperationMatrixHTML(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "operations.html")
	if err := os.WriteFile(path, []byte(operationMatrixHTML), 0o600); err != nil {
		t.Fatalf("write operation fixture: %v", err)
	}
	return "file://" + path
}

func locateConcreteElement(t *testing.T, d *Driver, ctx context.Context, selectorType fingerprint.SelectorType, value string) *Element {
	t.Helper()
	found, err := d.Locate(ctx, fingerprint.NodeSpec{ID: value, Selectors: []fingerprint.Selector{{Type: selectorType, Value: value}}})
	if err != nil {
		t.Fatalf("Locate(%s, %q): %v", selectorType, value, err)
	}
	element, ok := found.(*Element)
	if !ok {
		t.Fatalf("Locate(%s, %q) returned %T, want *Element", selectorType, value, found)
	}
	return element
}

func TestDriverAndElementOperationMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	d, err := New(Options{Headless: true, LocateTimeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = d.Close()
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixtureURL := writeOperationMatrixHTML(t)
	if err := d.Navigate(ctx, fixtureURL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	t.Run("screenshot validates quality context and page", func(t *testing.T) {
		image, err := d.CaptureViewportJPEG(ctx, 75)
		if err != nil {
			t.Fatalf("CaptureViewportJPEG: %v", err)
		}
		if len(image) < 2 || image[0] != 0xff || image[1] != 0xd8 {
			t.Fatalf("CaptureViewportJPEG returned %d non-JPEG bytes", len(image))
		}
		for _, quality := range []int{-1, 101} {
			if _, err := d.CaptureViewportJPEG(ctx, quality); err == nil {
				t.Fatalf("quality %d unexpectedly succeeded", quality)
			}
		}
		var unavailable *Driver
		if _, err := unavailable.CaptureViewportJPEG(ctx, 75); err == nil {
			t.Fatal("nil driver screenshot unexpectedly succeeded")
		}
		cancelled, cancelNow := context.WithCancel(context.Background())
		cancelNow()
		if _, err := d.CaptureViewportJPEG(cancelled, 75); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled screenshot error = %v, want context.Canceled", err)
		}
	})

	t.Run("selectors honor priority fallback and exact text target", func(t *testing.T) {
		selectors := []fingerprint.Selector{
			{Type: fingerprint.SelectorCSS, Value: "#priority-high", Priority: 10},
			{Type: fingerprint.SelectorXPath, Value: `//*[@id="priority-low"]`, Priority: 0},
		}
		found, err := d.Locate(ctx, fingerprint.NodeSpec{ID: "priority", Selectors: selectors})
		if err != nil {
			t.Fatalf("priority Locate: %v", err)
		}
		id, ok, err := found.Attribute(ctx, "id")
		if err != nil || !ok || id != "priority-low" {
			t.Fatalf("priority located id = %q, ok=%v, err=%v", id, ok, err)
		}
		if selectors[0].Value != "#priority-high" || selectors[1].Value != `//*[@id="priority-low"]` {
			t.Fatalf("Locate mutated selectors: %#v", selectors)
		}

		fallback, err := d.Locate(ctx, fingerprint.NodeSpec{ID: "fallback", Selectors: []fingerprint.Selector{
			{Type: fingerprint.SelectorCSS, Value: "#missing", Priority: 0},
			{Type: fingerprint.SelectorTestID, Value: "also-missing", Priority: 1},
			{Type: fingerprint.SelectorCSS, Value: "#priority-high", Priority: 2},
		}})
		if err != nil {
			t.Fatalf("fallback Locate: %v", err)
		}
		if id, _, _ := fallback.Attribute(ctx, "id"); id != "priority-high" {
			t.Fatalf("fallback located id = %q", id)
		}

		textTarget := locateConcreteElement(t, d, ctx, fingerprint.SelectorText, `^Exact target$`)
		if id, _, _ := textTarget.Attribute(ctx, "id"); id != "text-target" {
			t.Fatalf("text selector located id = %q, want closest matching target", id)
		}

		for _, tc := range []struct {
			name string
			sel  fingerprint.Selector
		}{
			{name: "invalid text regex", sel: fingerprint.Selector{Type: fingerprint.SelectorText, Value: "["}},
			{name: "unsupported type", sel: fingerprint.Selector{Type: fingerprint.SelectorType("shadow"), Value: "#x"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := d.Locate(ctx, fingerprint.NodeSpec{ID: tc.name, Selectors: []fingerprint.Selector{tc.sel}})
				if err == nil || errors.Is(err, node.ErrElementNotFound) {
					t.Fatalf("error = %v, want non-not-found selector error", err)
				}
			})
		}
		_, err = d.Locate(ctx, fingerprint.NodeSpec{ID: "missing text", Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorText, Value: "never present"}}})
		if !errors.Is(err, node.ErrElementNotFound) {
			t.Fatalf("missing text error = %v, want ErrElementNotFound", err)
		}
	})

	t.Run("element query input hover and native select", func(t *testing.T) {
		prefilled := locateConcreteElement(t, d, ctx, fingerprint.SelectorCSS, "#prefilled")
		if value, ok, err := prefilled.Attribute(ctx, "value"); err != nil || !ok || value != "seed" {
			t.Fatalf("value attribute = %q, ok=%v, err=%v", value, ok, err)
		}
		if value, ok, err := prefilled.Attribute(ctx, "data-absent"); err != nil || ok || value != "" {
			t.Fatalf("absent attribute = %q, ok=%v, err=%v", value, ok, err)
		}
		if err := prefilled.Input(ctx, "replacement"); err != nil {
			t.Fatalf("Input: %v", err)
		}
		state, err := prefilled.ValidationState(ctx)
		if err != nil || state.Value != "replacement" {
			t.Fatalf("input value = %q, err=%v; want replacement", state.Value, err)
		}

		hidden := locateConcreteElement(t, d, ctx, fingerprint.SelectorCSS, "#hidden")
		if visible, err := hidden.Visible(ctx); err != nil || visible {
			t.Fatalf("hidden Visible = %v, err=%v", visible, err)
		}
		hover := locateConcreteElement(t, d, ctx, fingerprint.SelectorCSS, "#hover")
		if err := hover.Hover(ctx); err != nil {
			t.Fatalf("Hover: %v", err)
		}
		if value, ok, err := hover.Attribute(ctx, "data-hovered"); err != nil || !ok || value != "yes" {
			t.Fatalf("hover evidence = %q, ok=%v, err=%v", value, ok, err)
		}

		single := locateConcreteElement(t, d, ctx, fingerprint.SelectorCSS, "#single")
		if err := single.Select(ctx, "United"); err != nil {
			t.Fatalf("single Select: %v", err)
		}
		state, err = single.ValidationState(ctx)
		if err != nil || len(state.SelectedValues) != 1 || state.SelectedValues[0] != "exact" {
			t.Fatalf("single selected values = %#v, err=%v", state.SelectedValues, err)
		}

		multiple := locateConcreteElement(t, d, ctx, fingerprint.SelectorCSS, "#multiple")
		if err := multiple.Select(ctx, "Beta", "Gamma"); err != nil {
			t.Fatalf("multiple Select: %v", err)
		}
		state, err = multiple.ValidationState(ctx)
		if err != nil || strings.Join(state.SelectedValues, ",") != "b,c" {
			t.Fatalf("multiple selected values = %#v, err=%v; want exact replacement set", state.SelectedValues, err)
		}
		before := append([]string(nil), state.SelectedValues...)
		if err := multiple.Select(ctx, "Beta", "missing"); err == nil {
			t.Fatal("partial native selection unexpectedly succeeded")
		}
		state, err = multiple.ValidationState(ctx)
		if err != nil || strings.Join(state.SelectedValues, ",") != strings.Join(before, ",") {
			t.Fatalf("failed native selection mutated state to %#v, err=%v", state.SelectedValues, err)
		}
		if err := multiple.Select(ctx, "Beta", ""); err == nil {
			t.Fatal("empty secondary option unexpectedly succeeded")
		}
		if err := hover.Select(ctx, "anything"); err == nil {
			t.Fatal("unsupported element Select unexpectedly succeeded")
		}
	})

	t.Run("validation state covers native aria contenteditable and Ant controls", func(t *testing.T) {
		cases := []struct {
			selector string
			check    func(t *testing.T, state node.ValidationState)
		}{
			{selector: "#checked", check: func(t *testing.T, state node.ValidationState) {
				if !state.Enabled || !state.Checked || state.Mixed {
					t.Fatalf("checked state = %+v", state)
				}
			}},
			{selector: "#mixed", check: func(t *testing.T, state node.ValidationState) {
				if state.Enabled || !state.Mixed {
					t.Fatalf("mixed ARIA state = %+v", state)
				}
			}},
			{selector: "#pressed", check: func(t *testing.T, state node.ValidationState) {
				if !state.Pressed {
					t.Fatalf("pressed state = %+v", state)
				}
			}},
			{selector: "#editable", check: func(t *testing.T, state node.ValidationState) {
				if state.Value != "editable value" {
					t.Fatalf("contenteditable state = %+v", state)
				}
			}},
			{selector: "#ant", check: func(t *testing.T, state node.ValidationState) {
				if state.Enabled || strings.Join(state.SelectedTexts, ",") != "North,South" || strings.Join(state.SelectedValues, ",") != "north,south" {
					t.Fatalf("Ant state = %+v", state)
				}
			}},
		}
		for _, tc := range cases {
			t.Run(tc.selector, func(t *testing.T) {
				state, err := locateConcreteElement(t, d, ctx, fingerprint.SelectorCSS, tc.selector).ValidationState(ctx)
				if err != nil {
					t.Fatalf("ValidationState: %v", err)
				}
				tc.check(t, state)
			})
		}
		cancelled, cancelNow := context.WithCancel(context.Background())
		cancelNow()
		_, err := locateConcreteElement(t, d, ctx, fingerprint.SelectorCSS, "#checked").ValidationState(cancelled)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled ValidationState error = %v, want context.Canceled", err)
		}
	})

	t.Run("snapshot selectors relocate their exact candidates", func(t *testing.T) {
		snapshot, err := d.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		candidates, err := snapshot.Candidates(ctx)
		if err != nil {
			t.Fatalf("Candidates: %v", err)
		}
		want := map[string]bool{
			"deep-first":       false,
			"deep-second":      false,
			"duplicate-first":  false,
			"duplicate-second": false,
		}
		for _, candidate := range candidates {
			marker := candidate.Fingerprint.Attributes["data-snapshot-marker"]
			if _, tracked := want[marker]; !tracked {
				continue
			}
			found, err := d.Locate(ctx, fingerprint.NodeSpec{ID: marker, Selectors: []fingerprint.Selector{candidate.Selector}})
			if err != nil {
				t.Fatalf("relocate %s with %q: %v", marker, candidate.Selector.Value, err)
			}
			actual, ok, err := found.Attribute(ctx, "data-snapshot-marker")
			if err != nil || !ok || actual != marker {
				t.Fatalf("selector %q for %s relocated %q, ok=%v, err=%v", candidate.Selector.Value, marker, actual, ok, err)
			}
			want[marker] = true
		}
		for marker, found := range want {
			if !found {
				t.Errorf("snapshot candidate %s was not checked", marker)
			}
		}
		cancelled, cancelNow := context.WithCancel(context.Background())
		cancelNow()
		if _, err := snapshot.Candidates(cancelled); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled Candidates error = %v, want context.Canceled", err)
		}
	})

	t.Run("script bridge and document injection", func(t *testing.T) {
		stop, err := d.Expose("__healixOperationBridge", func(gson.JSON) (interface{}, error) { return "pong", nil })
		if err != nil {
			t.Fatalf("Expose: %v", err)
		}
		if err := d.EvalScript(ctx, `(async function () { window.__bridgeResult = await window.__healixOperationBridge({value: 1}); })()`); err != nil {
			t.Fatalf("EvalScript binding: %v", err)
		}
		result, err := d.RawPage().Context(ctx).Eval(`() => window.__bridgeResult`)
		if err != nil || result.Value.Str() != "pong" {
			t.Fatalf("bridge result = %q, err=%v", result.Value.Str(), err)
		}
		if err := stop(); err != nil {
			t.Fatalf("stop exposed binding: %v", err)
		}
		if err := d.EvalScript(ctx, `throw new Error("operation boom")`); err == nil || !strings.Contains(err.Error(), "eval script exception") {
			t.Fatalf("EvalScript exception error = %v", err)
		}

		remove, err := d.EvalOnNewDocument(`window.__newDocumentMarker = "installed"`)
		if err != nil {
			t.Fatalf("EvalOnNewDocument: %v", err)
		}
		if err := d.Open(ctx, fixtureURL); err != nil {
			t.Fatalf("Open after document injection: %v", err)
		}
		result, err = d.RawPage().Context(ctx).Eval(`() => window.__newDocumentMarker`)
		if err != nil || result.Value.Str() != "installed" {
			t.Fatalf("new document marker = %q, err=%v", result.Value.Str(), err)
		}
		if err := remove(); err != nil {
			t.Fatalf("remove document injection: %v", err)
		}
		if err := d.Open(ctx, fixtureURL); err != nil {
			t.Fatalf("Open after injection removal: %v", err)
		}
		result, err = d.RawPage().Context(ctx).Eval(`() => typeof window.__newDocumentMarker`)
		if err != nil || result.Value.Str() != "undefined" {
			t.Fatalf("removed document marker type = %q, err=%v", result.Value.Str(), err)
		}
	})

	t.Run("network wait press cancellation and lifecycle", func(t *testing.T) {
		if err := d.WaitNetworkIdle(ctx); err != nil {
			t.Fatalf("WaitNetworkIdle: %v", err)
		}
		cancelled, cancelNow := context.WithCancel(context.Background())
		cancelNow()
		if err := d.WaitNetworkIdle(cancelled); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled WaitNetworkIdle error = %v, want context.Canceled", err)
		}
		if err := d.Press(ctx, "Space"); err == nil {
			t.Fatal("unsupported key unexpectedly succeeded")
		}
		if err := d.Press(ctx, "Esc"); err != nil {
			t.Fatalf("Press Esc: %v", err)
		}
		if d.Closed() {
			t.Fatal("driver reported closed before Close")
		}
		select {
		case <-d.Done():
			t.Fatal("Done closed before Close")
		default:
		}
		waitCtx, cancelWait := context.WithCancel(context.Background())
		cancelWait()
		if err := d.Wait(waitCtx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Wait cancellation error = %v, want context.Canceled", err)
		}
		if err := d.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		closed = true
		if !d.Closed() {
			t.Fatal("driver did not report closed after Close")
		}
		select {
		case <-d.Done():
		case <-time.After(time.Second):
			t.Fatal("Done did not close after Close")
		}
		if err := d.Wait(context.Background()); err != nil {
			t.Fatalf("Wait after Close: %v", err)
		}
	})
}

func TestNewRejectsInvalidBrowserPath(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "missing-chromium")
	d, err := New(Options{Headless: true, BrowserPath: invalid})
	if err == nil {
		_ = d.Close()
		t.Fatal("New with missing BrowserPath unexpectedly succeeded")
	}
}
