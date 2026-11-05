package app

import (
	"github.com/keshon/gektor/internal/providers"
	"testing"
)

func TestReasoningValidationUsesCatalog(t *testing.T) {
	options := providers.ReasoningOptions{Efforts: []string{"low", "future-level"}, Summary: true}
	if err := validateReasoning(options, "future-level", true); err != nil {
		t.Fatal(err)
	}
	if err := validateReasoning(options, "high", false); err == nil {
		t.Fatal("accepted effort absent from catalog")
	}
	if err := validateReasoning(providers.ReasoningOptions{}, "", true); err == nil {
		t.Fatal("accepted unsupported summary")
	}
	if err := validateReasoning(providers.ReasoningOptions{}, "", false); err != nil {
		t.Fatal(err)
	}
}
