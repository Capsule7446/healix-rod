package sampler

import (
	"testing"

	"github.com/ysmood/gson"
)

func BenchmarkDecodeCaptureScalar(b *testing.B) {
	payload := gson.New(validCapturePayload())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := decodeCapture(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeCaptureList(b *testing.B) {
	payloadMap := validCapturePayload()
	payloadMap["action"].(map[string]any)["value"] = []string{"one", "two", "three", "four"}
	payload := gson.New(payloadMap)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := decodeCapture(payload); err != nil {
			b.Fatal(err)
		}
	}
}
