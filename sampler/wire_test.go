package sampler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ysmood/gson"

	"github.com/Capsule7446/healix-core/domain/sampling"
)

func validCapturePayload() map[string]any {
	return map[string]any{
		"protocol_version": captureProtocolVersion,
		"capture_id":       "capture-1",
		"node_uuid":        "known-node",
		"identity_key":     "https://example.test/login|css:#username",
		"page_url":         "https://example.test/login",
		"action": map[string]any{
			"kind":  "input",
			"value": "alice",
			"hints": map[string]any{"optional": true, "intent": "fill_identity"},
		},
		"node": map[string]any{
			"uuid":     "sampled-node",
			"id":       "pending",
			"page_url": "https://example.test/login",
			"origin":   "sampled",
			"role":     "textbox",
			"selectors": []any{
				map[string]any{"type": "role", "value": `textbox[name="Username"]`, "priority": 0},
				map[string]any{"type": "css", "value": "#username", "priority": 1},
			},
			"fingerprint": map[string]any{
				"tag":           "input",
				"attributes":    map[string]any{"id": "username", "type": "text"},
				"text":          "",
				"aria":          map[string]any{"role": "textbox", "name": "Username"},
				"path":          []any{"html", "body", "input#username"},
				"sibling_index": 2,
				"neighbors":     map[string]any{"prev": "label", "next": "button", "parent_tag": "body"},
				"label_text":    "Username",
				"form_id":       "login",
			},
		},
	}
}

func TestDecodeCaptureMapsWireACL(t *testing.T) {
	payload := validCapturePayload()
	capture, err := decodeCapture(gson.New(payload))
	if err != nil {
		t.Fatalf("decodeCapture: %v", err)
	}
	if capture.CaptureID != "capture-1" || capture.NodeUUID != "known-node" || capture.Kind != sampling.ActionInput || capture.Value != "alice" {
		t.Fatalf("capture scalar fields = %+v", capture)
	}
	if !capture.Hints.Optional || capture.Hints.Intent != "fill_identity" {
		t.Fatalf("capture hints = %+v", capture.Hints)
	}
	if capture.Spec.ID != "" || capture.Spec.UUID != "sampled-node" || capture.Spec.Role != "textbox" || len(capture.Spec.Selectors) != 2 {
		t.Fatalf("captured spec identity = %+v", capture.Spec)
	}
	if capture.Spec.Fingerprint.Attributes["id"] != "username" || capture.Spec.Fingerprint.ARIA.Name != "Username" ||
		capture.Spec.Fingerprint.SiblingIndex != 2 || capture.Spec.Fingerprint.Neighbors.ParentTag != "body" ||
		capture.Spec.Fingerprint.LabelText != "Username" || capture.Spec.Fingerprint.FormID != "login" {
		t.Fatalf("captured fingerprint = %+v", capture.Spec.Fingerprint)
	}
}

func TestDecodeCaptureActionValueShapes(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		wireValues []string
		wantValue  string
		wantValues string
	}{
		{name: "scalar", value: "one", wireValues: []string{"kept"}, wantValue: "one", wantValues: "kept"},
		{name: "list", value: []string{"one", "two"}, wantValue: "one", wantValues: "one,two"},
		{name: "empty list", value: []string{}, wantValue: "", wantValues: ""},
		{name: "null", value: nil, wireValues: []string{"kept"}, wantValue: "", wantValues: "kept"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := validCapturePayload()
			action := payload["action"].(map[string]any)
			action["value"] = tc.value
			if tc.wireValues != nil {
				action["values"] = tc.wireValues
			}
			capture, err := decodeCapture(gson.New(payload))
			if err != nil {
				t.Fatalf("decodeCapture: %v", err)
			}
			if capture.Value != tc.wantValue || strings.Join(capture.Values, ",") != tc.wantValues {
				t.Fatalf("value=%q values=%#v, want %q/%q", capture.Value, capture.Values, tc.wantValue, tc.wantValues)
			}
		})
	}
}

func TestDecodeCapturePressBypassesNodeAndValidationCaptureMapsSemantics(t *testing.T) {
	pressPayload := map[string]any{
		"protocol_version": captureProtocolVersion,
		"capture_id":       "press-1",
		"page_url":         "https://example.test",
		"action":           map[string]any{"kind": "press", "value": "Enter"},
	}
	press, err := decodeCapture(gson.New(pressPayload))
	if err != nil {
		t.Fatalf("decode press: %v", err)
	}
	if press.Kind != sampling.ActionPress || press.Value != "Enter" || press.Spec.ID != "" || len(press.Spec.Selectors) != 0 {
		t.Fatalf("press capture = %+v", press)
	}

	validatePayload := validCapturePayload()
	validatePayload["action"] = map[string]any{
		"kind": "validate",
		"validation": map[string]any{
			"kind":            "selected_set_equals",
			"expected":        `["North","South"]`,
			"actual":          `["North","South"]`,
			"attribute":       "data-state",
			"supported_kinds": []string{"selected_set_equals", "selected_set_contains"},
			"sensitive":       true,
		},
	}
	validation, err := decodeCapture(gson.New(validatePayload))
	if err != nil {
		t.Fatalf("decode validation: %v", err)
	}
	if validation.Kind != sampling.ActionValidate || validation.Validation == nil || validation.Validation.Kind != "selected_set_equals" ||
		validation.Validation.Expected != `["North","South"]` || validation.Validation.Attribute != "data-state" ||
		!validation.Validation.Sensitive || strings.Join(validation.Validation.SupportedKinds, ",") != "selected_set_equals,selected_set_contains" {
		t.Fatalf("validation capture = %+v", validation)
	}
}

func TestDecodeCaptureRejectsMalformedEnvelopeAndActionValue(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{name: "unknown envelope field", mutate: func(payload map[string]any) { payload["future"] = true }, wantErr: "unknown field"},
		{name: "unsupported protocol", mutate: func(payload map[string]any) { payload["protocol_version"] = "2.0" }, wantErr: "unsupported capture protocol"},
		{name: "object action value", mutate: func(payload map[string]any) {
			payload["action"].(map[string]any)["value"] = map[string]any{"bad": true}
		}, wantErr: "action.value must be string or string list"},
		{name: "boolean action value", mutate: func(payload map[string]any) { payload["action"].(map[string]any)["value"] = true }, wantErr: "action.value must be string or string list"},
		{name: "unknown action field", mutate: func(payload map[string]any) { payload["action"].(map[string]any)["future"] = true }, wantErr: "unknown field"},
		{name: "unknown validation field", mutate: func(payload map[string]any) {
			payload["action"].(map[string]any)["validation"] = map[string]any{"kind": "visible", "future": true}
		}, wantErr: "unknown field"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := validCapturePayload()
			tc.mutate(payload)
			_, err := decodeCapture(gson.New(payload))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}

	var action actionDTO
	if err := json.Unmarshal([]byte(`{"kind":"input","value":["ok",1]}`), &action); err == nil || !strings.Contains(err.Error(), "action.value") {
		t.Fatalf("mixed list action error = %v", err)
	}
}

func TestDecodeCaptureRejectsInvalidCapturedNodeMatrix(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{name: "missing id", mutate: func(node map[string]any) { node["id"] = "" }, wantErr: "id is required"},
		{name: "missing selectors", mutate: func(node map[string]any) { node["selectors"] = []any{} }, wantErr: "selectors must contain"},
		{name: "unsupported selector", mutate: func(node map[string]any) {
			node["selectors"] = []any{map[string]any{"type": "shadow", "value": "#x", "priority": 0}}
		}, wantErr: "type must be one of"},
		{name: "empty selector value", mutate: func(node map[string]any) {
			node["selectors"] = []any{map[string]any{"type": "css", "value": "", "priority": 0}}
		}, wantErr: "value is required"},
		{name: "missing selector priority", mutate: func(node map[string]any) { node["selectors"] = []any{map[string]any{"type": "css", "value": "#x"}} }, wantErr: "priority is required"},
		{name: "negative selector priority", mutate: func(node map[string]any) {
			node["selectors"] = []any{map[string]any{"type": "css", "value": "#x", "priority": -1}}
		}, wantErr: "priority must be >= 0"},
		{name: "missing fingerprint tag", mutate: func(node map[string]any) { node["fingerprint"].(map[string]any)["tag"] = "" }, wantErr: "fingerprint.tag is required"},
		{name: "missing fingerprint attributes", mutate: func(node map[string]any) { node["fingerprint"].(map[string]any)["attributes"] = nil }, wantErr: "fingerprint.attributes is required"},
		{name: "negative sibling index", mutate: func(node map[string]any) { node["fingerprint"].(map[string]any)["sibling_index"] = -1 }, wantErr: "sibling_index must be >= 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := validCapturePayload()
			tc.mutate(payload["node"].(map[string]any))
			_, err := decodeCapture(gson.New(payload))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}

	payload := validCapturePayload()
	node := payload["node"].(map[string]any)
	node["id"] = ""
	node["selectors"] = []any{}
	node["fingerprint"].(map[string]any)["tag"] = ""
	_, err := decodeCapture(gson.New(payload))
	if err == nil || !strings.Contains(err.Error(), "id is required; selectors must contain") || !strings.Contains(err.Error(), "fingerprint.tag is required") {
		t.Fatalf("aggregated validation error = %v", err)
	}
}
