// Package sampler implements interactive sampling in a Rod-controlled browser.
package sampler

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ysmood/gson"

	"github.com/Capsule7446/healix-core/domain/fingerprint"
	"github.com/Capsule7446/healix-core/domain/sampling"
)

//go:embed assets/sampler.js
var samplerJS string

type Options struct {
	Headless    bool
	BrowserPath string
}

const samplingCloseTimeout = 6 * time.Second

type captureDTO struct {
	ProtocolVersion string      `json:"protocol_version"`
	CaptureID       string      `json:"capture_id"`
	NodeUUID        string      `json:"node_uuid"`
	IdentityKey     string      `json:"identity_key"`
	PageURL         string      `json:"page_url"`
	Action          actionDTO   `json:"action"`
	Node            nodeSpecDTO `json:"node"`
}

type actionDTO struct {
	Kind       string        `json:"kind"`
	Value      string        `json:"value"`
	Values     []string      `json:"values"`
	Hints      hintsDTO      `json:"hints"`
	Validation validationDTO `json:"validation"`
}

// validationDTO intentionally mirrors only the generic semantic recommendation
// produced in sampler.js.  Ant Design DOM details are consumed there and never
// cross this infrastructure boundary.
type validationDTO struct {
	Kind           string   `json:"kind"`
	Expected       string   `json:"expected"`
	Actual         string   `json:"actual"`
	Attribute      string   `json:"attribute"`
	SupportedKinds []string `json:"supported_kinds"`
	Sensitive      bool     `json:"sensitive"`
}

type hintsDTO struct {
	Optional bool   `json:"optional"`
	Intent   string `json:"intent"`
}

func (a *actionDTO) UnmarshalJSON(data []byte) error {
	type rawAction struct {
		Kind       string          `json:"kind"`
		Value      json.RawMessage `json:"value"`
		Values     []string        `json:"values"`
		Hints      hintsDTO        `json:"hints"`
		Validation validationDTO   `json:"validation"`
	}
	var raw rawAction
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	a.Kind = raw.Kind
	a.Value = ""
	a.Values = append([]string(nil), raw.Values...)
	a.Hints = raw.Hints
	a.Validation = raw.Validation
	if len(raw.Value) == 0 || string(raw.Value) == "null" {
		return nil
	}
	var scalar string
	if err := json.Unmarshal(raw.Value, &scalar); err == nil {
		a.Value = scalar
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw.Value, &list); err != nil {
		return fmt.Errorf("action.value must be string or string list")
	}
	a.Values = list
	if len(list) > 0 {
		a.Value = list[0]
	}
	return nil
}

func decodeCapture(payload gson.JSON) (sampling.Capture, error) {
	encoded, err := payload.MarshalJSON()
	if err != nil {
		return sampling.Capture{}, fmt.Errorf("sampler: marshal binding payload: %w", err)
	}
	var dto captureDTO
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dto); err != nil {
		return sampling.Capture{}, fmt.Errorf("sampler: decode binding payload: %w", err)
	}
	if dto.ProtocolVersion != captureProtocolVersion {
		return sampling.Capture{}, fmt.Errorf("sampler: unsupported capture protocol %q", dto.ProtocolVersion)
	}
	var spec fingerprint.NodeSpec
	if dto.Action.Kind != string(sampling.ActionPress) {
		if err := validateCapturedNode(dto.Node); err != nil {
			return sampling.Capture{}, fmt.Errorf("sampler: validate captured node: %w", err)
		}
		spec = capturedNodeSpec(dto.Node)
		spec.ID = ""
	}
	capture := sampling.Capture{
		CaptureID: dto.CaptureID, NodeUUID: dto.NodeUUID, IdentityKey: dto.IdentityKey, PageURL: dto.PageURL,
		Kind: sampling.ActionKind(dto.Action.Kind), Value: dto.Action.Value, Values: append([]string(nil), dto.Action.Values...),
		Hints: sampling.ActionHints{Optional: dto.Action.Hints.Optional, Intent: dto.Action.Hints.Intent},
		Spec:  spec,
	}
	if capture.Kind == sampling.ActionValidate {
		capture.Validation = &sampling.ValidationSample{Kind: dto.Action.Validation.Kind,
			Expected: dto.Action.Validation.Expected, Actual: dto.Action.Validation.Actual,
			Attribute: dto.Action.Validation.Attribute, SupportedKinds: append([]string(nil), dto.Action.Validation.SupportedKinds...),
			Sensitive: dto.Action.Validation.Sensitive}
	}
	return capture, nil
}

const stopSamplerScript = `(function () {
  if (typeof window.__healixSamplerStop === "function") return window.__healixSamplerStop();
})();`
