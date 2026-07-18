package rod

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"github.com/Capsule7446/healix-core/domain/node"
)

// Element 包装一个 go-rod 的 *rod.Element，以满足 domain/node.Element。
type Element struct {
	el *rod.Element
}

const elementStableTimeout = 15 * time.Second

func (e *Element) Exists(ctx context.Context) (bool, error) {
	// isConnected distinguishes a detached DOM node from transport, context and
	// browser failures without translating arbitrary CDP errors into absence.
	result, err := e.el.Context(ctx).Eval(`() => this.isConnected`)
	if err != nil {
		if errors.Is(err, &rod.ObjectNotFoundError{}) {
			return false, fmt.Errorf("%w: element handle is detached", node.ErrElementNotFound)
		}
		return false, err
	}
	return result.Value.Bool(), nil
}

func (e *Element) Visible(ctx context.Context) (bool, error) {
	return e.el.Context(ctx).Visible()
}

func (e *Element) Text(ctx context.Context) (string, error) {
	return e.el.Context(ctx).Text()
}

func (e *Element) Attribute(ctx context.Context, name string) (string, bool, error) {
	v, err := e.el.Context(ctx).Attribute(name)
	if err != nil {
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	return *v, true, nil
}

// ValidationState is the Rod implementation of node.ValidationStateReader.
// It reads live DOM properties first and standard ARIA attributes second, so
// native controls and supported framework controls share one semantic result.
func (e *Element) ValidationState(ctx context.Context) (node.ValidationState, error) {
	result, err := e.el.Context(ctx).Eval(`() => {
		const bool = (value) => value === true || String(value || '').toLowerCase() === 'true';
		const selectedOptions = this && this.selectedOptions ? Array.from(this.selectedOptions) : [];
		const attr = (name) => this.getAttribute ? this.getAttribute(name) : null;
		// Ant Design v4/v5 adaptation remains confined to this Rod adapter.
		// All callers receive only the standard state below.
		const antSelect = this.closest && this.closest('.ant-select') || (this.classList && this.classList.contains('ant-select') ? this : null);
		const antSelected = antSelect ? Array.from(antSelect.querySelectorAll('.ant-select-selection-item')).map((item) => String(item.innerText || item.textContent || '').replace(/\s+/g, ' ').trim()) : [];
		const antValues = antSelect ? Array.from(antSelect.querySelectorAll('.ant-select-selection-item')).map((item) => String(item.getAttribute('title') || item.dataset.value || item.innerText || item.textContent || '').replace(/\s+/g, ' ').trim()) : [];
		const antChecked = this.classList && this.classList.contains('ant-switch-checked');
		const ariaChecked = String(attr('aria-checked') || '').toLowerCase();
		const ariaSelected = String(attr('aria-selected') || '').toLowerCase();
		const ariaPressed = String(attr('aria-pressed') || '').toLowerCase();
		const ariaDisabled = String(attr('aria-disabled') || '').toLowerCase();
		return {
			value: this.isContentEditable ? (this.innerText || this.textContent || '') : String(this.value ?? ''),
			enabled: !(this.disabled === true || ariaDisabled === 'true' || (antSelect && antSelect.classList.contains('ant-select-disabled'))),
			checked: bool(this.checked) || ariaChecked === 'true' || antChecked,
			mixed: bool(this.indeterminate) || ariaChecked === 'mixed',
			selected: bool(this.selected) || ariaSelected === 'true',
			pressed: ariaPressed === 'true',
			selectedTexts: selectedOptions.length ? selectedOptions.map((option) => String(option.innerText || option.textContent || '').replace(/\s+/g, ' ').trim()) : antSelected,
			selectedValues: selectedOptions.length ? selectedOptions.map((option) => String(option.value || '')) : antValues,
		};
	}`)
	if err != nil {
		return node.ValidationState{}, err
	}
	var raw struct {
		Value          string   `json:"value"`
		Enabled        bool     `json:"enabled"`
		Checked        bool     `json:"checked"`
		Mixed          bool     `json:"mixed"`
		Selected       bool     `json:"selected"`
		Pressed        bool     `json:"pressed"`
		SelectedTexts  []string `json:"selectedTexts"`
		SelectedValues []string `json:"selectedValues"`
	}
	if err := result.Value.Unmarshal(&raw); err != nil {
		return node.ValidationState{}, fmt.Errorf("decode validation state: %w", err)
	}
	return node.ValidationState{Value: raw.Value, Enabled: raw.Enabled, Checked: raw.Checked, Mixed: raw.Mixed,
		Selected: raw.Selected, Pressed: raw.Pressed, SelectedTexts: raw.SelectedTexts, SelectedValues: raw.SelectedValues}, nil
}

func (e *Element) Click(ctx context.Context) error {
	err := e.el.Context(ctx).Click(proto.InputMouseButtonLeft, 1)
	if err == nil || !strings.Contains(err.Error(), "pointer-events is none") {
		return err
	}
	if fallbackErr := e.clickPointerEnabledAncestor(ctx); fallbackErr != nil {
		return errors.Join(err, fallbackErr)
	}
	return nil
}

func (e *Element) clickPointerEnabledAncestor(ctx context.Context) error {
	result, err := e.el.Context(ctx).Eval(`() => {
		let current = this;
		while (current && window.getComputedStyle(current).pointerEvents === "none") {
			current = current.parentElement;
		}
		if (!current || typeof current.click !== "function") return false;
		current.click();
		return true;
	}`)
	if err != nil {
		return fmt.Errorf("click pointer-enabled ancestor: %w", err)
	}
	if !result.Value.Bool() {
		return fmt.Errorf("click pointer-enabled ancestor: no clickable ancestor")
	}
	return nil
}

func (e *Element) Input(ctx context.Context, text string) error {
	el := e.el.Context(ctx)
	if err := el.SelectAllText(); err != nil {
		return err
	}
	return el.Input(text)
}

// Select 按可见文本选中原生 <select> 或受支持的自定义下拉控件。
func (e *Element) Select(ctx context.Context, option string, more ...string) error {
	options := append([]string{option}, more...)
	return selectVisibleText(ctx, e.el, options)
}

// Hover 把鼠标移到元素上，触发 mouseover/mouseenter。
func (e *Element) Hover(ctx context.Context) error {
	return e.el.Context(ctx).Hover()
}

// WaitStable 在执行操作前阻塞等待，直到该元素的位置/尺寸不再变化
// （方案 §10.3，稳定性等待）。
func (e *Element) WaitStable(ctx context.Context) error {
	err := e.el.Context(ctx).Timeout(elementStableTimeout).WaitStable(150 * time.Millisecond)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	exists, existsErr := e.Exists(ctx)
	if existsErr != nil || !exists {
		return err
	}
	visible, visibleErr := e.Visible(ctx)
	if visibleErr != nil || !visible {
		return err
	}
	return nil
}
