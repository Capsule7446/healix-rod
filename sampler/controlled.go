package sampler

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ysmood/gson"

	"github.com/Capsule7446/healix-core/domain/sampling"
	rodadapter "github.com/Capsule7446/healix-rod"
)

// ControlledBrowser keeps the interactive browser lifetime independent from
// the capture lifetime. It is used by the desktop sampling workspace, where a
// user may log in before sampling and pause without losing the browser profile.
type ControlledBrowser struct {
	driver *rodadapter.Driver

	mu           sync.Mutex
	handler      sampling.CaptureHandler
	opened       bool
	closed       bool
	stopExpose   func() error
	removeScript func() error
}

func NewControlled(opts Options) (*ControlledBrowser, error) {
	driver, err := rodadapter.New(rodadapter.Options{
		Headless: opts.Headless, BrowserPath: opts.BrowserPath,
	})
	if err != nil {
		return nil, err
	}
	return &ControlledBrowser{driver: driver}, nil
}

// Open installs the sampler in a disabled state before navigating. Page
// interactions therefore remain invisible until BeginCapture is called.
func (b *ControlledBrowser) Open(ctx context.Context, startURL string) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("sampler: browser is closed")
	}
	if b.opened {
		b.mu.Unlock()
		return errors.New("sampler: browser is already open")
	}
	b.opened = true
	b.mu.Unlock()

	stopExpose, err := b.driver.Expose("__healixCaptureNode", func(payload gson.JSON) (interface{}, error) {
		b.mu.Lock()
		handler := b.handler
		closed := b.closed
		b.mu.Unlock()
		if closed || handler == nil {
			return nil, errors.New("sampler: capture is paused")
		}
		capture, err := decodeCapture(payload)
		if err != nil {
			return nil, err
		}
		result, err := handler(capture)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"session_id": result.SessionID, "capture_id": result.CaptureID,
			"node_uuid": result.NodeUUID, "node_id": result.NodeID,
			"action_uuid": result.ActionUUID, "sequence": result.Sequence, "created": result.Created,
		}, nil
	})
	if err != nil {
		_ = b.driver.Close()
		return fmt.Errorf("sampler: expose capture binding: %w", err)
	}
	combinedScript := "window.__healixSamplerInitialCaptureEnabled = false;\n" + samplerJS
	removeScript, err := b.driver.EvalOnNewDocument(combinedScript)
	if err != nil {
		return errors.Join(fmt.Errorf("sampler: register controlled capture script: %w", err), stopExpose(), b.driver.Close())
	}
	b.mu.Lock()
	b.stopExpose = stopExpose
	b.removeScript = removeScript
	b.mu.Unlock()
	if err := b.driver.EvalScript(ctx, combinedScript); err != nil {
		return errors.Join(fmt.Errorf("sampler: inject controlled capture script: %w", err), b.Close())
	}
	if err := b.driver.Open(ctx, startURL); err != nil {
		return errors.Join(err, b.Close())
	}
	return nil
}

func (b *ControlledBrowser) BeginCapture(ctx context.Context, handler sampling.CaptureHandler) error {
	if handler == nil {
		return errors.New("sampler: capture handler is required")
	}
	b.mu.Lock()
	if b.closed || !b.opened {
		b.mu.Unlock()
		return errors.New("sampler: browser is not open")
	}
	b.handler = handler
	b.mu.Unlock()
	if err := b.driver.EvalScript(ctx, `(async function () {
  if (typeof window.__healixSamplerSetCaptureEnabled !== "function") throw new Error("sampler is unavailable");
  await window.__healixSamplerSetCaptureEnabled(true);
})();`); err != nil {
		b.mu.Lock()
		b.handler = nil
		b.mu.Unlock()
		return fmt.Errorf("sampler: start capture: %w", err)
	}
	return nil
}

func (b *ControlledBrowser) PauseCapture(ctx context.Context) error {
	b.mu.Lock()
	if b.closed || !b.opened {
		b.mu.Unlock()
		return errors.New("sampler: browser is not open")
	}
	b.mu.Unlock()
	err := b.driver.EvalScript(ctx, `(async function () {
  if (typeof window.__healixSamplerSetCaptureEnabled !== "function") throw new Error("sampler is unavailable");
  await window.__healixSamplerSetCaptureEnabled(false);
})();`)
	b.mu.Lock()
	b.handler = nil
	b.mu.Unlock()
	if err != nil {
		return fmt.Errorf("sampler: pause capture: %w", err)
	}
	return nil
}

// ArmValidationCapture switches the page-side sampler into a one-shot target
// picker.  The injected script intercepts the complete next left pointer
// sequence before page handlers run and forwards exactly one `validate`
// capture through the ordinary binding.
func (b *ControlledBrowser) ArmValidationCapture(ctx context.Context) error {
	b.mu.Lock()
	if b.closed || !b.opened || b.handler == nil {
		b.mu.Unlock()
		return errors.New("sampler: capture is not running")
	}
	b.mu.Unlock()
	if err := b.driver.EvalScript(ctx, `(function () {
  if (typeof window.__healixSamplerSetValidationArmed !== "function") throw new Error("validation sampler is unavailable");
  return window.__healixSamplerSetValidationArmed(true);
})();`); err != nil {
		return fmt.Errorf("sampler: arm validation capture: %w", err)
	}
	return nil
}

func (b *ControlledBrowser) CancelValidationCapture(ctx context.Context) error {
	b.mu.Lock()
	if b.closed || !b.opened {
		b.mu.Unlock()
		return errors.New("sampler: browser is not open")
	}
	b.mu.Unlock()
	if err := b.driver.EvalScript(ctx, `(function () {
  if (typeof window.__healixSamplerSetValidationArmed !== "function") return false;
  return window.__healixSamplerSetValidationArmed(false);
})();`); err != nil {
		return fmt.Errorf("sampler: cancel validation capture: %w", err)
	}
	return nil
}

func (b *ControlledBrowser) CurrentURL() (string, error) {
	info, err := b.driver.RawPage().Info()
	if err != nil {
		return "", fmt.Errorf("sampler: read current URL: %w", err)
	}
	return info.URL, nil
}

func (b *ControlledBrowser) Done() <-chan struct{} { return b.driver.Done() }

func (b *ControlledBrowser) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.handler = nil
	opened := b.opened
	stopExpose := b.stopExpose
	removeScript := b.removeScript
	b.mu.Unlock()

	var result error
	if opened && !b.driver.Closed() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), samplingCloseTimeout)
		result = errors.Join(result, b.driver.EvalScript(cleanupCtx, stopSamplerScript))
		cancel()
	}
	if removeScript != nil {
		result = errors.Join(result, removeScript())
	}
	if stopExpose != nil {
		result = errors.Join(result, stopExpose())
	}
	return errors.Join(result, b.driver.Close())
}
