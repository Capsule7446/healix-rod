package sampler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Capsule7446/healix-core/domain/fingerprint"
	"github.com/Capsule7446/healix-core/domain/sampling"
)

const samplerActionMatrixHTML = `<!doctype html>
<html><body>
  <label for="named">Visible label</label>
  <input id="named" aria-label="ARIA override">
  <input id="password" type="password" aria-label="Password">
  <input id="file" type="file" aria-label="Upload">
  <input id="enter" aria-label="Search">
  <button id="close" aria-label="Close">x</button>
  <input id="checked" type="checkbox" checked aria-label="Enabled">
  <button id="pressed" aria-pressed="true">Pressed</button>
  <select id="single" aria-label="Region">
    <option>North</option><option>South</option>
  </select>
  <select id="multiple" multiple aria-label="Tags">
    <option selected>Alpha</option><option>Beta</option><option>Gamma</option>
  </select>

  <div id="ant-single" class="ant-select ant-select-open">
    <div class="ant-select-selector"><input role="combobox" aria-label="Audience"></div>
  </div>
  <div id="ant-single-dropdown" class="ant-select-dropdown">
    <div id="ant-single-option" class="ant-select-item-option"><div class="ant-select-item-option-content">Verified</div></div>
  </div>

  <div id="ant-multiple" class="ant-select ant-select-open ant-select-multiple">
    <div class="ant-select-selector"><input role="combobox" aria-label="Segments"></div>
  </div>
  <div id="ant-multiple-dropdown" class="ant-select-dropdown">
    <div id="ant-multiple-one" class="ant-select-item-option"><div class="ant-select-item-option-content">Retail</div></div>
    <div id="ant-multiple-two" class="ant-select-item-option"><div class="ant-select-item-option-content">VIP</div></div>
  </div>
</body></html>`

func nextSamplerCapture(t *testing.T, ctx context.Context, captures <-chan sampling.Capture) sampling.Capture {
	t.Helper()
	select {
	case capture := <-captures:
		return capture
	case <-ctx.Done():
		t.Fatalf("wait sampler capture: %v", ctx.Err())
		return sampling.Capture{}
	}
}

func selectorValue(spec fingerprint.NodeSpec, selectorType fingerprint.SelectorType) string {
	for _, selector := range spec.Selectors {
		if selector.Type == selectorType {
			return selector.Value
		}
	}
	return ""
}

func TestSamplerPageActionMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	path := filepath.Join(t.TempDir(), "sampler-actions.html")
	if err := os.WriteFile(path, []byte(samplerActionMatrixHTML), 0o600); err != nil {
		t.Fatal(err)
	}
	browser, err := NewControlled(Options{Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = browser.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := browser.Open(ctx, "file://"+path); err != nil {
		t.Fatal(err)
	}
	if _, err := browser.driver.RawPage().Context(ctx).Element("#named"); err != nil {
		t.Fatal(err)
	}
	captures := make(chan sampling.Capture, 32)
	if err := browser.BeginCapture(ctx, func(capture sampling.Capture) (sampling.CaptureResult, error) {
		captures <- capture
		return sampling.CaptureResult{NodeUUID: "known-node", NodeID: "known-node"}, nil
	}); err != nil {
		t.Fatal(err)
	}

	t.Run("input uses browser accessible-name precedence", func(t *testing.T) {
		if err := browser.driver.EvalScript(ctx, `(() => {
          const input = document.querySelector('#named');
          input.value = 'alice';
          input.dispatchEvent(new InputEvent('input', {bubbles: true, composed: true}));
        })()`); err != nil {
			t.Fatal(err)
		}
		capture := nextSamplerCapture(t, ctx, captures)
		if capture.Kind != sampling.ActionInput || capture.Value != "alice" || capture.Spec.Fingerprint.ARIA.Name != "ARIA override" {
			t.Fatalf("named input capture = %+v", capture)
		}
		if got := selectorValue(capture.Spec, fingerprint.SelectorRole); got != `textbox[name="ARIA override"]` {
			t.Fatalf("role selector = %q", got)
		}
	})

	t.Run("sensitive and file inputs use placeholders", func(t *testing.T) {
		if err := browser.driver.EvalScript(ctx, `(() => {
          const password = document.querySelector('#password');
          password.value = 'never-persist-me';
          password.dispatchEvent(new InputEvent('input', {bubbles: true, composed: true}));
        })()`); err != nil {
			t.Fatal(err)
		}
		password := nextSamplerCapture(t, ctx, captures)
		if password.Kind != sampling.ActionInput || password.Value != "${PASSWORD}" {
			t.Fatalf("password capture = %+v", password)
		}
		if err := browser.driver.EvalScript(ctx, `document.querySelector('#file').dispatchEvent(new Event('change', {bubbles: true, composed: true}))`); err != nil {
			t.Fatal(err)
		}
		file := nextSamplerCapture(t, ctx, captures)
		if file.Kind != sampling.ActionInput || file.Value != "${FILE}" {
			t.Fatalf("file capture = %+v", file)
		}
	})

	t.Run("native select captures scalar and exact multiple set", func(t *testing.T) {
		if err := browser.driver.EvalScript(ctx, `(() => {
          const select = document.querySelector('#single');
          select.selectedIndex = 1;
          select.dispatchEvent(new Event('change', {bubbles: true, composed: true}));
        })()`); err != nil {
			t.Fatal(err)
		}
		single := nextSamplerCapture(t, ctx, captures)
		if single.Kind != sampling.ActionSelect || single.Value != "South" || len(single.Values) != 0 {
			t.Fatalf("single select capture = %+v", single)
		}
		if err := browser.driver.EvalScript(ctx, `(() => {
          const options = document.querySelector('#multiple').options;
          options[0].selected = false;
          options[1].selected = true;
          options[2].selected = true;
          document.querySelector('#multiple').dispatchEvent(new Event('change', {bubbles: true, composed: true}));
        })()`); err != nil {
			t.Fatal(err)
		}
		multiple := nextSamplerCapture(t, ctx, captures)
		if multiple.Kind != sampling.ActionSelect || multiple.Value != "Beta" || strings.Join(multiple.Values, ",") != "Beta,Gamma" {
			t.Fatalf("multiple select capture = %+v", multiple)
		}
	})

	t.Run("Ant select adapter captures single and batches multiple", func(t *testing.T) {
		if err := browser.driver.EvalScript(ctx, `(() => {
          document.querySelector('#ant-single').dispatchEvent(new MouseEvent('click', {bubbles: true, button: 0}));
          document.querySelector('#ant-single-option').dispatchEvent(new MouseEvent('click', {bubbles: true, button: 0}));
        })()`); err != nil {
			t.Fatal(err)
		}
		single := nextSamplerCapture(t, ctx, captures)
		if single.Kind != sampling.ActionSelect || single.Value != "Verified" || single.Spec.Role != "combobox" || single.Spec.Fingerprint.ARIA.Name != "Audience" {
			t.Fatalf("Ant single capture = %+v", single)
		}
		if err := browser.driver.EvalScript(ctx, `(() => {
          document.querySelector('#ant-multiple').dispatchEvent(new MouseEvent('click', {bubbles: true, button: 0}));
          document.querySelector('#ant-multiple-one').dispatchEvent(new MouseEvent('click', {bubbles: true, button: 0}));
          document.querySelector('#ant-multiple-two').dispatchEvent(new MouseEvent('click', {bubbles: true, button: 0}));
        })()`); err != nil {
			t.Fatal(err)
		}
		multiple := nextSamplerCapture(t, ctx, captures)
		if multiple.Kind != sampling.ActionSelect || multiple.Value != "Retail" || strings.Join(multiple.Values, ",") != "Retail,VIP" || multiple.Spec.Fingerprint.ARIA.Name != "Segments" {
			t.Fatalf("Ant multiple capture = %+v", multiple)
		}
	})

	t.Run("press ordering and optional hint", func(t *testing.T) {
		if err := browser.driver.EvalScript(ctx, `(() => {
          const input = document.querySelector('#enter');
          input.value = 'query';
          input.dispatchEvent(new InputEvent('input', {bubbles: true, composed: true}));
          input.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', code: 'Enter', bubbles: true}));
        })()`); err != nil {
			t.Fatal(err)
		}
		input := nextSamplerCapture(t, ctx, captures)
		press := nextSamplerCapture(t, ctx, captures)
		if input.Kind != sampling.ActionInput || input.Value != "query" || press.Kind != sampling.ActionPress || press.Value != "Enter" {
			t.Fatalf("Enter capture order = %+v then %+v", input, press)
		}
		if err := browser.driver.EvalScript(ctx, `document.body.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', code: 'Escape', bubbles: true}))`); err != nil {
			t.Fatal(err)
		}
		escape := nextSamplerCapture(t, ctx, captures)
		if escape.Kind != sampling.ActionPress || escape.Value != "Escape" {
			t.Fatalf("Escape capture = %+v", escape)
		}
		if err := browser.driver.EvalScript(ctx, `document.querySelector('#close').click()`); err != nil {
			t.Fatal(err)
		}
		closeCapture := nextSamplerCapture(t, ctx, captures)
		if closeCapture.Kind != sampling.ActionClick || !closeCapture.Hints.Optional || closeCapture.Hints.Intent != "close_overlay" {
			t.Fatalf("optional close capture = %+v", closeCapture)
		}
	})

	t.Run("validation picker recommends control semantics", func(t *testing.T) {
		tests := []struct {
			selector string
			kind     string
		}{
			{selector: "#checked", kind: "checked"},
			{selector: "#pressed", kind: "pressed"},
			{selector: "#multiple", kind: "selected_set_equals"},
			{selector: "#named", kind: "value_equals"},
		}
		for _, tc := range tests {
			if err := browser.ArmValidationCapture(ctx); err != nil {
				t.Fatalf("ArmValidationCapture(%s): %v", tc.selector, err)
			}
			script := `(() => {
              const target = document.querySelector(` + "`" + tc.selector + "`" + `);
              for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click']) {
                target.dispatchEvent(new MouseEvent(type, {bubbles: true, cancelable: true, button: 0, view: window}));
              }
            })()`
			if err := browser.driver.EvalScript(ctx, script); err != nil {
				t.Fatal(err)
			}
			capture := nextSamplerCapture(t, ctx, captures)
			if capture.Kind != sampling.ActionValidate || capture.Validation == nil || capture.Validation.Kind != tc.kind {
				t.Fatalf("validation %s capture = %+v", tc.selector, capture)
			}
		}
	})

	if err := browser.PauseCapture(ctx); err != nil {
		t.Fatal(err)
	}
}
