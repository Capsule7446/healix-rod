package contract_test

import (
	"testing"

	"github.com/ysmood/gson"

	"github.com/Capsule7446/healix-core/domain/node"
	"github.com/Capsule7446/healix-core/domain/sampling"
	rodadapter "github.com/Capsule7446/healix-rod"
	"github.com/Capsule7446/healix-rod/sampler"
)

func TestPublicPortsRemainImplemented(t *testing.T) {
	var _ node.Driver = (*rodadapter.Driver)(nil)
	var _ node.Element = (*rodadapter.Element)(nil)
	var _ sampling.CaptureHandler = func(sampling.Capture) (sampling.CaptureResult, error) {
		return sampling.CaptureResult{}, nil
	}
	var _ = (*sampler.ControlledBrowser)(nil)
	var _ = func(string, func(gson.JSON) (interface{}, error)) {}

	options := rodadapter.Options{Headless: true}
	if !options.Headless {
		t.Fatal("public Options did not preserve configuration")
	}
}
