package sampler

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Capsule7446/healix-core/domain/fingerprint"
)

const captureProtocolVersion = "1.0"

type nodeSpecDTO struct {
	UUID        string         `json:"uuid,omitempty"`
	ID          string         `json:"id"`
	PageURL     string         `json:"page_url,omitempty"`
	Origin      string         `json:"origin,omitempty"`
	Role        string         `json:"role"`
	Selectors   []selectorDTO  `json:"selectors"`
	Fingerprint fingerprintDTO `json:"fingerprint"`
}

type selectorDTO struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Priority *int   `json:"priority"`
}

type fingerprintDTO struct {
	Tag          string            `json:"tag"`
	Attributes   map[string]string `json:"attributes"`
	Text         string            `json:"text"`
	ARIA         ariaDTO           `json:"aria"`
	Path         []string          `json:"path"`
	SiblingIndex int               `json:"sibling_index"`
	Neighbors    neighborsDTO      `json:"neighbors"`
	LabelText    string            `json:"label_text,omitempty"`
	FormID       string            `json:"form_id,omitempty"`
}

type ariaDTO struct {
	Role string `json:"role"`
	Name string `json:"name"`
}

type neighborsDTO struct {
	Prev      string `json:"prev"`
	Next      string `json:"next"`
	ParentTag string `json:"parent_tag"`
}

var supportedCaptureSelectorTypes = map[string]bool{
	"role": true, "testid": true, "css": true, "xpath": true, "text": true,
}

func validateCapturedNode(dto nodeSpecDTO) error {
	var problems []string
	if dto.ID == "" {
		problems = append(problems, "id is required")
	}
	if len(dto.Selectors) == 0 {
		problems = append(problems, "selectors must contain at least 1 item")
	}
	for index, selector := range dto.Selectors {
		prefix := fmt.Sprintf("selectors[%d]", index)
		if !supportedCaptureSelectorTypes[selector.Type] {
			problems = append(problems, fmt.Sprintf("%s.type must be one of [role testid css xpath text], got %q", prefix, selector.Type))
		}
		if selector.Value == "" {
			problems = append(problems, prefix+".value is required")
		}
		if selector.Priority == nil {
			problems = append(problems, prefix+".priority is required")
		} else if *selector.Priority < 0 {
			problems = append(problems, fmt.Sprintf("%s.priority must be >= 0", prefix))
		}
	}
	if dto.Fingerprint.Tag == "" {
		problems = append(problems, "fingerprint.tag is required")
	}
	if dto.Fingerprint.Attributes == nil {
		problems = append(problems, "fingerprint.attributes is required")
	}
	if dto.Fingerprint.SiblingIndex < 0 {
		problems = append(problems, "fingerprint.sibling_index must be >= 0")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func capturedNodeSpec(dto nodeSpecDTO) fingerprint.NodeSpec {
	selectors := make([]fingerprint.Selector, 0, len(dto.Selectors))
	for _, selector := range dto.Selectors {
		priority := 0
		if selector.Priority != nil {
			priority = *selector.Priority
		}
		selectors = append(selectors, fingerprint.Selector{Type: fingerprint.SelectorType(selector.Type),
			Value: selector.Value, Priority: priority})
	}
	fp := dto.Fingerprint
	return fingerprint.NodeSpec{UUID: dto.UUID, ID: dto.ID, PageURL: dto.PageURL, Origin: dto.Origin,
		Role: dto.Role, Selectors: selectors, Fingerprint: fingerprint.Fingerprint{
			Tag: fp.Tag, Attributes: fp.Attributes, Text: fp.Text,
			ARIA: fingerprint.ARIA{Role: fp.ARIA.Role, Name: fp.ARIA.Name}, Path: fp.Path,
			SiblingIndex: fp.SiblingIndex, Neighbors: fingerprint.Neighbors{Prev: fp.Neighbors.Prev,
				Next: fp.Neighbors.Next, ParentTag: fp.Neighbors.ParentTag},
			LabelText: fp.LabelText, FormID: fp.FormID,
		}}
}
