package rod

import (
	"fmt"
	"regexp"

	"github.com/go-rod/rod"
)

// locateByText finds an element whose visible text matches a regular expression.
// go-rod 的 ElementR 在整份文档里按 DOM 序找第一个 innerText 匹配的元素，
// 而祖先节点（html/body/…）的 innerText 是全部后代文本的聚合，天然包含
// Matching descendant text can otherwise be returned before the intended target element
// 在非单元素页面上基本定位不到预期目标。这里改为收集全部命中元素，取
// 匹配文本最短的一个：祖先的命中文本必然是后代命中文本的超集（更长），
// 所以最短命中就是最贴近目标的具体元素。
func locateByText(p *rod.Page, pattern string) (*rod.Element, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid text selector pattern %q: %w", pattern, err)
	}

	all, err := p.Elements("*")
	if err != nil {
		return nil, err
	}

	var best *rod.Element
	bestLen := -1
	for _, el := range all {
		txt, err := el.Text()
		if err != nil {
			return nil, err
		}
		if !re.MatchString(txt) {
			continue
		}
		if bestLen == -1 || len(txt) < bestLen {
			best, bestLen = el, len(txt)
			continue
		}
		// A wrapper with a single matching child has exactly the same innerText
		// length. Prefer the descendant so a text selector resolves the concrete
		// target instead of its aggregate-text ancestor.
		if len(txt) == bestLen {
			contains, err := best.ContainsElement(el)
			if err != nil {
				return nil, err
			}
			if contains {
				best = el
			}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: no element matches text pattern %q", errSelectorNotFound, pattern)
	}
	return best, nil
}
