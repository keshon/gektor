package settings

import (
	"testing"

	"charm.land/bubbles/v2/textarea"

	"github.com/keshon/gektor/internal/config"
	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
	"github.com/keshon/gektor/internal/tui/components/form"
)

func TestPolicyRetainsCopiedSnapshotValuesAndFeedback(t *testing.T) {
	snapshot := fieldform.Snapshot{
		Fields:      []form.Field{{Key: "model", Value: "original"}},
		FieldErrors: map[string]string{"model": "original error"},
	}
	policy := Model{}
	policy.Prepare(snapshot)
	snapshot.Fields[0].Value = "changed after preparation"
	snapshot.FieldErrors["model"] = "changed error"
	if policy.Fields[0].Value != "original" || policy.FieldErrors["model"] != "original error" {
		t.Fatal("settings policy retained mutable snapshot storage")
	}
	policy.Fields[0].Value = "policy edit"
	policy.FieldErrors["model"] = "policy error"
	if snapshot.Fields[0].Value != "changed after preparation" || snapshot.FieldErrors["model"] != "changed error" {
		t.Fatal("settings policy changed its caller's snapshot")
	}
}

func TestPolicyPresentationCannotMutateConfiguredControls(t *testing.T) {
	controls := fieldform.NewModel([]form.Field{{Key: "kind", Value: "llama"}, {Key: "endpoint", Value: "http://127.0.0.1:9931"}}, textarea.New())
	controls.Focus(1, nil)
	policy := Model{}
	presentation := policy.Prepare(controls.Snapshot())
	controls.Configure(presentation)
	policy.Fields[1].Value = "policy-only value"
	presentation.Choices["kind"][0] = "changed choice"
	presentation.FieldLabels["endpoint"] = "changed label"
	if controls.Editor.Value() != "http://127.0.0.1:9931" || controls.Fields[1].Value != "http://127.0.0.1:9931" || controls.FieldLabels["endpoint"] != "Server address" || controls.Choices["kind"][0] == "changed choice" {
		t.Fatal("configured controls retained settings-owned presentation storage")
	}
	if controls.Index != 1 {
		t.Fatal("presentation changed focused field")
	}
}

func TestPolicyUsesStagedProviderForSnapshotVisibilityAndDirtyState(t *testing.T) {
	p := profile()
	draft := New(p)
	_, connection := PageFields(p, "model", config.Preferences{})
	draft.Visit("model", connection)
	for i := range connection {
		if connection[i].Key == "kind" {
			connection[i].Value = "chatgpt"
		}
	}
	draft.Store("model", connection)
	_, fields := PageFields(p, "generation", config.Preferences{})
	draft.Visit("generation", fields)
	policy := Model{EditSession: &EditSession{Draft: draft, Section: "generation"}}
	presentation := policy.Prepare(fieldform.Snapshot{Fields: fields})
	if !presentation.Hidden["temperature"] || !presentation.Hidden["max"] || presentation.Hidden["reasoning-effort"] {
		t.Fatal("snapshot visibility ignored the staged provider")
	}
	if presentation.DisablePrimary {
		t.Fatal("a dirty staged model page disabled applying the complete draft")
	}
	if p.Provider.Kind != "llama" || draft.Base().Provider.Kind != "llama" {
		t.Fatal("snapshot policy applied a staged provider before submission")
	}
}
