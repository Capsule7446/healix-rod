package rod

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-rod/rod"
)

type selectControlStrategy interface {
	Select(ctx context.Context, el *rod.Element, options []string) (handled bool, err error)
}

var selectControlStrategies = []selectControlStrategy{
	nativeSelectStrategy{},
	antSelectStrategy{},
}

func selectVisibleText(ctx context.Context, el *rod.Element, options []string) error {
	if len(options) == 0 {
		return fmt.Errorf("select visible text: option is required")
	}
	for _, option := range options {
		if strings.TrimSpace(option) == "" {
			return fmt.Errorf("select visible text: option is required")
		}
	}
	for _, strategy := range selectControlStrategies {
		handled, err := strategy.Select(ctx, el, options)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
	}
	return fmt.Errorf("select visible text %q: unsupported element", options)
}

type nativeSelectStrategy struct{}

func (nativeSelectStrategy) Select(ctx context.Context, el *rod.Element, options []string) (bool, error) {
	result, err := el.Context(ctx).Eval(`() => this.tagName && this.tagName.toLowerCase() === "select"`)
	if err != nil {
		return false, err
	}
	if !result.Value.Bool() {
		return false, nil
	}
	selection, err := el.Context(ctx).Eval(`(wantedValues) => {
		const normalize = (value) => String(value || "").replace(/\s+/g, " ").trim();
		const wanted = wantedValues.map(normalize);
		if (!this.multiple && wanted.length > 1) return {status: "multiple_not_supported"};
		const available = Array.from(this.options || []);
		const matched = [];
		for (const text of wanted) {
			const option = available.find((candidate) => normalize(candidate.innerText || candidate.textContent) === text);
			if (!option) return {status: "option_not_found", option: text};
			matched.push(option);
		}
		const selected = new Set(matched);
		for (const option of available) option.selected = selected.has(option);
		this.dispatchEvent(new Event("input", {bubbles: true}));
		this.dispatchEvent(new Event("change", {bubbles: true}));
		return {status: "selected"};
	}`, options)
	if err != nil {
		return true, err
	}
	var outcome struct {
		Status string `json:"status"`
		Option string `json:"option"`
	}
	if err := selection.Value.Unmarshal(&outcome); err != nil {
		return true, fmt.Errorf("select visible text %q: decode native select result: %w", options, err)
	}
	switch outcome.Status {
	case "selected":
		return true, nil
	case "option_not_found":
		return true, fmt.Errorf("select visible text %q: native option %q not found", options, outcome.Option)
	case "multiple_not_supported":
		return true, fmt.Errorf("select visible text %q: multiple options require a multiple select", options)
	default:
		return true, fmt.Errorf("select visible text %q: unexpected native select result %q", options, outcome.Status)
	}
}

type antSelectStrategy struct{}

func (antSelectStrategy) Select(ctx context.Context, el *rod.Element, options []string) (bool, error) {
	result, err := el.Context(ctx).Eval(`async (wantedValues) => {
		const normalize = (value) => String(value || "").replace(/\s+/g, " ").trim();
		const visible = (node) => {
			if (!node || !node.isConnected) return false;
			const style = window.getComputedStyle(node);
			if (style.display === "none" || style.visibility === "hidden") return false;
			const rect = node.getBoundingClientRect();
			return rect.width > 0 && rect.height > 0;
		};
		const fire = (node, type) => {
			node.dispatchEvent(new MouseEvent(type, {
				bubbles: true,
				cancelable: true,
				view: window,
				button: 0,
				buttons: type === "mouseup" || type === "click" ? 0 : 1
			}));
		};
		const findVisibleOption = async (wantedText) => {
			const deadline = Date.now() + 2000;
			while (Date.now() <= deadline) {
				const candidates = Array.from(document.querySelectorAll(
					".ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option:not(.ant-select-item-option-disabled)"
				)).filter(visible);
				const option = candidates.find((candidate) => normalize(candidate.innerText || candidate.textContent) === wantedText);
				if (option) return option;
				await new Promise((resolve) => setTimeout(resolve, 50));
			}
			return null;
		};
		const wantedList = Array.isArray(wantedValues) ? wantedValues : [wantedValues];
		const root = this.closest && this.closest(".ant-select") || (
			this.classList && this.classList.contains("ant-select") ? this : null
		);
		if (!root || root.classList.contains("ant-select-disabled")) return "unsupported";

		const trigger = root.querySelector(".ant-select-selector, [role=combobox], input") || root;
		for (const wanted of wantedList) {
			fire(trigger, "mousedown");
			fire(trigger, "mouseup");
			fire(trigger, "click");
			const wantedText = normalize(wanted);
			const option = await findVisibleOption(wantedText);
			if (!option) return "option_not_found:" + wantedText;

			const target = option.querySelector(".ant-select-item-option-content") || option;
			fire(target, "mousemove");
			fire(target, "mousedown");
			fire(target, "mouseup");
			fire(target, "click");
			await new Promise((resolve) => setTimeout(resolve, 80));
		}
		return "selected";
	}`, options)
	if err != nil {
		return false, err
	}
	outcome := result.Value.Str()
	switch {
	case outcome == "unsupported":
		return false, nil
	case outcome == "selected":
		return true, nil
	case strings.HasPrefix(outcome, "option_not_found:"):
		return true, fmt.Errorf("select visible text %q: Ant Design option not found", options)
	default:
		return true, fmt.Errorf("select visible text %q: unexpected Ant Design select result %q", options, outcome)
	}
}
