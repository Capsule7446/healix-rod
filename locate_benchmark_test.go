package rod

import (
	"testing"

	"github.com/Capsule7446/healix-core/domain/fingerprint"
)

func BenchmarkSortedSelectorsSmall(b *testing.B) {
	selectors := []fingerprint.Selector{
		{Type: fingerprint.SelectorCSS, Value: "#last", Priority: 3},
		{Type: fingerprint.SelectorRole, Value: "button", Priority: 1},
		{Type: fingerprint.SelectorText, Value: "Submit", Priority: 2},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = sortedSelectors(selectors)
	}
}

func BenchmarkSortedSelectorsLarge(b *testing.B) {
	selectors := make([]fingerprint.Selector, 32)
	for i := range selectors {
		selectors[i] = fingerprint.Selector{Type: fingerprint.SelectorCSS, Value: "#candidate", Priority: len(selectors) - i}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = sortedSelectors(selectors)
	}
}
