package sampler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Capsule7446/healix-core/domain/sampling"
)

func TestControlledBrowserPausesWithoutClosingPage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that launches a real Chromium in -short mode")
	}
	htmlPath := filepath.Join(t.TempDir(), "controlled.html")
	if err := os.WriteFile(htmlPath, []byte(`<!doctype html><input id="name" aria-label="Name"><button id="submit">Submit</button><a id="validate-link" href="#should-not-navigate" onpointerdown="this.dataset.pointerdown='yes'" onclick="this.dataset.clicked='yes'">订单成功</a>`), 0o600); err != nil {
		t.Fatal(err)
	}
	browser, err := NewControlled(Options{Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = browser.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	handler := sampling.CaptureHandler(func(sampling.Capture) (sampling.CaptureResult, error) {
		return sampling.CaptureResult{}, nil
	})
	if err := browser.BeginCapture(ctx, nil); err == nil {
		t.Fatal("BeginCapture accepted a nil handler")
	}
	if err := browser.BeginCapture(ctx, handler); err == nil {
		t.Fatal("BeginCapture succeeded before Open")
	}
	if err := browser.PauseCapture(ctx); err == nil {
		t.Fatal("PauseCapture succeeded before Open")
	}
	if err := browser.ArmValidationCapture(ctx); err == nil {
		t.Fatal("ArmValidationCapture succeeded before Open")
	}
	if err := browser.CancelValidationCapture(ctx); err == nil {
		t.Fatal("CancelValidationCapture succeeded before Open")
	}
	if err := browser.Open(ctx, "file://"+htmlPath); err != nil {
		t.Fatal(err)
	}
	if err := browser.Open(ctx, "file://"+htmlPath); err == nil {
		t.Fatal("second Open unexpectedly succeeded")
	}
	if currentURL, err := browser.CurrentURL(); err != nil || currentURL != "file://"+htmlPath {
		t.Fatalf("CurrentURL = %q, err=%v", currentURL, err)
	}
	select {
	case <-browser.Done():
		t.Fatal("Done closed while controlled browser was open")
	default:
	}
	// Open deliberately returns after navigation instead of waiting for full DOM
	// stability. Synchronize this test with the element it immediately drives so
	// a slow concurrent Chromium package cannot make querySelector return nil.
	if _, err := browser.driver.RawPage().Context(ctx).Element("#submit"); err != nil {
		t.Fatalf("wait for controlled page: %v", err)
	}
	captures := make(chan sampling.Capture, 4)
	handler = sampling.CaptureHandler(func(capture sampling.Capture) (sampling.CaptureResult, error) {
		captures <- capture
		return sampling.CaptureResult{NodeUUID: "node", NodeID: "node"}, nil
	})
	if err := browser.driver.EvalScript(ctx, `document.querySelector('#submit').click()`); err != nil {
		t.Fatal(err)
	}
	select {
	case capture := <-captures:
		t.Fatalf("capture occurred before sampling started: %#v", capture)
	case <-time.After(250 * time.Millisecond):
	}
	if err := browser.BeginCapture(ctx, handler); err != nil {
		t.Fatal(err)
	}
	if err := browser.driver.EvalScript(ctx, `document.querySelector('#submit').click()`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-captures:
	case <-ctx.Done():
		t.Fatal("controlled browser did not capture while enabled")
	}
	if err := browser.driver.EvalScript(ctx, `(() => {
      const input = document.querySelector('#name');
      input.value = 'pending-before-pause';
      input.dispatchEvent(new InputEvent('input', {bubbles: true, composed: true}));
    })()`); err != nil {
		t.Fatal(err)
	}
	if err := browser.PauseCapture(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case capture := <-captures:
		if capture.Kind != sampling.ActionInput || capture.Value != "pending-before-pause" {
			t.Fatalf("pause flush capture = %+v", capture)
		}
	case <-ctx.Done():
		t.Fatal("pausing did not flush pending input")
	}
	if browser.driver.Closed() {
		t.Fatal("pausing capture closed the browser")
	}
	if err := browser.driver.EvalScript(ctx, `document.querySelector('#submit').click()`); err != nil {
		t.Fatal(err)
	}
	select {
	case capture := <-captures:
		t.Fatalf("capture occurred while paused: %#v", capture)
	case <-time.After(250 * time.Millisecond):
	}
	if err := browser.BeginCapture(ctx, handler); err != nil {
		t.Fatal(err)
	}
	if err := browser.driver.EvalScript(ctx, `document.querySelector('#submit').click()`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-captures:
	case <-ctx.Done():
		t.Fatal("controlled browser did not resume capture")
	}
	if err := browser.CancelValidationCapture(ctx); err != nil {
		t.Fatalf("CancelValidationCapture while disarmed: %v", err)
	}
	if err := browser.ArmValidationCapture(ctx); err != nil {
		t.Fatal(err)
	}
	if err := browser.CancelValidationCapture(ctx); err != nil {
		t.Fatalf("CancelValidationCapture while armed: %v", err)
	}
	if err := browser.ArmValidationCapture(ctx); err != nil {
		t.Fatal(err)
	}
	if err := browser.driver.EvalScript(ctx, `(() => {
      const link = document.querySelector('#validate-link');
      for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click']) {
        link.dispatchEvent(new MouseEvent(type, {bubbles: true, cancelable: true, button: 0, view: window}));
      }
    })()`); err != nil {
		t.Fatal(err)
	}
	select {
	case capture := <-captures:
		if capture.Kind != sampling.ActionValidate || capture.Validation == nil || capture.Validation.Kind != "text_equals" {
			t.Fatalf("validation capture = %#v", capture)
		}
		if capture.Spec.Fingerprint.Tag != "a" || capture.Validation.Expected != "订单成功" {
			t.Fatalf("validation semantic target = %#v", capture)
		}
	case <-ctx.Done():
		t.Fatal("controlled browser did not capture validation target")
	}
	if err := browser.driver.EvalScript(ctx, `(() => {
      const link = document.querySelector('#validate-link');
      if (link.dataset.pointerdown || link.dataset.clicked || location.hash) throw new Error('validation click reached page business handler');
    })()`); err != nil {
		t.Fatal(err)
	}
	if err := browser.PauseCapture(ctx); err != nil {
		t.Fatal(err)
	}
	if err := browser.ArmValidationCapture(ctx); err == nil {
		t.Fatal("ArmValidationCapture succeeded while paused")
	}
	if err := browser.PauseCapture(ctx); err != nil {
		t.Fatalf("second PauseCapture was not idempotent: %v", err)
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-browser.Done():
	case <-time.After(time.Second):
		t.Fatal("Done did not close after Close")
	}
	if err := browser.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := browser.BeginCapture(ctx, handler); err == nil {
		t.Fatal("BeginCapture succeeded after Close")
	}
	if err := browser.PauseCapture(ctx); err == nil {
		t.Fatal("PauseCapture succeeded after Close")
	}
	if err := browser.CancelValidationCapture(ctx); err == nil {
		t.Fatal("CancelValidationCapture succeeded after Close")
	}
	if _, err := browser.CurrentURL(); err == nil {
		t.Fatal("CurrentURL succeeded after Close")
	}
}
