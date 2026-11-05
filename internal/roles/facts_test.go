package roles

import (
	"context"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestFactsMergePreservesUnknownValues(t *testing.T) {
	env := Env{Model: "known", ContextLimit: 4096, Vision: true, RefreshModelFacts: func(context.Context) llm.ModelFacts { return llm.ModelFacts{Model: "new", ModelKnown: true} }}
	refreshed := env.Refreshed(t.Context())
	if refreshed.Model != "new" || refreshed.ContextLimit != 4096 || !refreshed.Vision || env.Model != "known" {
		t.Fatal("unknown facts overwrote known values")
	}
	env.RefreshModelFacts = func(context.Context) llm.ModelFacts { return llm.ModelFacts{Vision: false, VisionKnown: true} }
	if env.Refreshed(t.Context()).Vision {
		t.Fatal("explicit unsupported vision ignored")
	}
}
