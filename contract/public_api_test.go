package contract_test

import (
	"testing"

	"github.com/Capsule7446/healix-core/domain/node"
	rodadapter "github.com/Capsule7446/healix-rod"
)

func TestDriverImplementsCorePort(t *testing.T) {
	var _ node.Driver = (*rodadapter.Driver)(nil)
	options := rodadapter.Options{Headless: true}
	if !options.Headless {
		t.Fatal("public Options did not preserve configuration")
	}
}
