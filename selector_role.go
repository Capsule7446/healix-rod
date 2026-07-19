package rod

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// roleSelectorRe parses role selectors such as "button[name=登录]" or "button".
var roleSelectorRe = regexp.MustCompile(`^([a-zA-Z][\w-]*)(?:\[name=(.+)\])?$`)

func parseRoleSelector(value string) (role, name string, err error) {
	m := roleSelectorRe.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return "", "", fmt.Errorf("invalid role selector %q", value)
	}
	role = strings.ToLower(m[1])
	name = strings.TrimSpace(m[2])
	if len(name) >= 2 && ((name[0] == '"' && name[len(name)-1] == '"') || (name[0] == '\'' && name[len(name)-1] == '\'')) {
		name = name[1 : len(name)-1]
	}
	return role, name, nil
}

// locateByRole queries Chromium's accessibility tree, so both implicit roles
// and computed accessible names follow the browser's own semantics.
func locateByRole(p *rod.Page, value string) (*rod.Element, error) {
	role, name, err := parseRoleSelector(value)
	if err != nil {
		return nil, err
	}

	tree, err := proto.AccessibilityGetFullAXTree{}.Call(p)
	if err != nil {
		return nil, err
	}
	for _, axNode := range tree.Nodes {
		if axNode.Ignored || axNode.BackendDOMNodeID == 0 || axValue(axNode.Role) != role {
			continue
		}
		if name != "" && axValue(axNode.Name) != name {
			continue
		}
		return p.ElementFromNode(&proto.DOMNode{BackendNodeID: axNode.BackendDOMNodeID})
	}
	return nil, fmt.Errorf("%w: no element with role %q name %q", errSelectorNotFound, role, name)
}

func axValue(value *proto.AccessibilityAXValue) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(value.Value.Str())
}
