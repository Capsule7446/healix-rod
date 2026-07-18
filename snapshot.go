package rod

import (
	"context"
	"fmt"

	"github.com/go-rod/rod"

	"github.com/Capsule7446/healix-core/domain/fingerprint"
	"github.com/Capsule7446/healix-core/domain/heal"
)

// candidateJS 一次往返扫描所有可能可交互的元素
// （比逐元素发一次 CDP 调用更省成本），返回 domain/heal 打分器
// 所需的全部信息，外加一个合成的 CSS path，供 Driver.Locate
// 重新定位出胜出的候选。
const candidateJS = `() => {
  function uniqueID(el) {
    if (!el.id) return false;
    try {
      const matches = document.querySelectorAll('#' + CSS.escape(el.id));
      return matches.length === 1 && matches[0] === el;
    } catch (_) {
      return false;
    }
  }
  function cssPath(el) {
    if (uniqueID(el)) return '#' + CSS.escape(el.id);
    const parts = [];
    let cur = el;
    while (cur && cur.nodeType === 1) {
      let seg = cur.tagName.toLowerCase();
      if (cur.id) seg += '#' + CSS.escape(cur.id);
      const parent = cur.parentElement;
      if (parent) {
        const siblings = Array.from(parent.children).filter(c => c.tagName === cur.tagName);
        if (siblings.length > 1) seg += ':nth-of-type(' + (siblings.indexOf(cur) + 1) + ')';
      }
      parts.unshift(seg);
      if (uniqueID(cur)) break;
      cur = parent;
    }
    return parts.join(' > ');
  }
  function ancestorPath(el) {
    const p = [];
    let cur = el;
    while (cur && cur.nodeType === 1) {
      let seg = cur.tagName.toLowerCase();
      if (cur.id) seg += '#' + cur.id;
      p.unshift(seg);
      cur = cur.parentElement;
    }
    return p;
  }
  function labelText(el) {
    if (el.id) {
      const byFor = document.querySelector('label[for="' + CSS.escape(el.id) + '"]');
      if (byFor) return (byFor.innerText || byFor.textContent || '').trim();
    }
    const labelledBy = el.getAttribute('aria-labelledby');
    if (labelledBy) {
      const text = labelledBy.split(/\s+/).map((id) => {
        const ref = document.getElementById(id);
        return ref ? (ref.innerText || ref.textContent || '').trim() : '';
      }).filter(Boolean).join(' ');
      if (text) return text;
    }
    const wrapping = el.closest('label');
    if (wrapping) return (wrapping.innerText || wrapping.textContent || '').trim();
    return '';
  }
  function implicitRole(el) {
    const explicit = el.getAttribute('role');
    if (explicit) return explicit.trim().split(/\s+/)[0].toLowerCase();
    const tag = el.tagName.toLowerCase();
    if (tag === 'a' && el.hasAttribute('href')) return 'link';
    if (tag === 'button') return 'button';
    if (tag === 'textarea') return 'textbox';
    if (tag === 'select') return (el.multiple || el.size > 1) ? 'listbox' : 'combobox';
    if (tag === 'img' && el.getAttribute('alt') !== '') return 'img';
    if (/^h[1-6]$/.test(tag)) return 'heading';
    if (tag === 'input') {
      const type = (el.getAttribute('type') || 'text').toLowerCase();
      if (['button', 'image', 'reset', 'submit'].includes(type)) return 'button';
      if (type === 'checkbox') return 'checkbox';
      if (type === 'radio') return 'radio';
      if (type === 'range') return 'slider';
      if (type === 'number') return 'spinbutton';
      if (type === 'search') return 'searchbox';
      if (!['hidden', 'color', 'file'].includes(type)) return 'textbox';
    }
    return '';
  }
  function accessibleName(el) {
    const labelledBy = el.getAttribute('aria-labelledby');
    if (labelledBy) {
      const text = labelledBy.split(/\s+/).map((id) => {
        const ref = document.getElementById(id);
        return ref ? (ref.innerText || ref.textContent || '').trim() : '';
      }).filter(Boolean).join(' ');
      if (text) return text;
    }
    const ariaLabel = el.getAttribute('aria-label');
    if (ariaLabel) return ariaLabel.trim();
    const label = labelText(el);
    if (label) return label;
    const tag = el.tagName.toLowerCase();
    if (tag === 'img') return (el.getAttribute('alt') || '').trim();
    if (tag === 'input' && ['button', 'reset', 'submit'].includes((el.type || '').toLowerCase())) {
      return (el.value || '').trim();
    }
    return (el.innerText || el.textContent || el.getAttribute('title') || '').trim();
  }
  function formID(el) {
    const form = el.closest('form');
    if (!form) return '';
    return form.id || form.getAttribute('name') || '';
  }
  const out = [];
  document.querySelectorAll('body, body *').forEach((el) => {
    if (['script', 'style', 'template', 'noscript'].includes(el.tagName.toLowerCase())) return;
    const parent = el.parentElement;
    const siblings = parent ? Array.from(parent.children) : [el];
    const idx = siblings.indexOf(el);
    const attrs = {};
    for (const a of el.attributes) attrs[a.name] = a.value;
    const text = (el.innerText || el.textContent || '').trim();
    out.push({
      tag: el.tagName.toLowerCase(),
      attributes: attrs,
      text: text,
      ariaRole: implicitRole(el),
      ariaName: accessibleName(el),
      path: ancestorPath(el),
      siblingIndex: idx,
      prev: idx > 0 ? siblings[idx - 1].tagName.toLowerCase() : '',
      next: idx < siblings.length - 1 ? siblings[idx + 1].tagName.toLowerCase() : '',
      parentTag: parent ? parent.tagName.toLowerCase() : '',
      labelText: labelText(el),
      formId: formID(el),
      selector: cssPath(el),
    });
  });
  return out;
}`

type candidateJSON struct {
	Tag          string            `json:"tag"`
	Attributes   map[string]string `json:"attributes"`
	Text         string            `json:"text"`
	AriaRole     string            `json:"ariaRole"`
	AriaName     string            `json:"ariaName"`
	Path         []string          `json:"path"`
	SiblingIndex int               `json:"siblingIndex"`
	Prev         string            `json:"prev"`
	Next         string            `json:"next"`
	ParentTag    string            `json:"parentTag"`
	LabelText    string            `json:"labelText"`
	FormID       string            `json:"formId"`
	Selector     string            `json:"selector"`
}

// Snapshot 是 heal.DOMSnapshot 的 go-rod 实现。
type Snapshot struct {
	page *rod.Page
}

var _ heal.DOMSnapshot = (*Snapshot)(nil)

func (s *Snapshot) Candidates(ctx context.Context) ([]heal.SnapshotCandidate, error) {
	obj, err := s.page.Context(ctx).Eval(candidateJS)
	if err != nil {
		return nil, fmt.Errorf("scan candidates: %w", err)
	}

	var raw []candidateJSON
	if err := obj.Value.Unmarshal(&raw); err != nil {
		return nil, fmt.Errorf("decode candidates: %w", err)
	}

	out := make([]heal.SnapshotCandidate, 0, len(raw))
	for _, c := range raw {
		out = append(out, heal.SnapshotCandidate{
			Fingerprint: fingerprint.Fingerprint{
				Tag:          c.Tag,
				Attributes:   c.Attributes,
				Text:         c.Text,
				ARIA:         fingerprint.ARIA{Role: c.AriaRole, Name: c.AriaName},
				Path:         c.Path,
				SiblingIndex: c.SiblingIndex,
				Neighbors:    fingerprint.Neighbors{Prev: c.Prev, Next: c.Next, ParentTag: c.ParentTag},
				LabelText:    c.LabelText,
				FormID:       c.FormID,
			},
			Selector: fingerprint.Selector{Type: fingerprint.SelectorCSS, Value: c.Selector, Priority: 0},
		})
	}
	return out, nil
}
