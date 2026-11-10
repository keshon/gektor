package conversation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestPromptRevisionsPreserveChangesAndUnknownDelivery(t *testing.T) {
	s := New([]llm.Message{{Role: llm.RoleSystem, Content: "old"}})
	old := s.RecordPrompt("old", nil, 0)
	current := s.RecordPrompt("old", []llm.PromptSection{{Name: "identity", Text: "old"}}, 1)
	if old.Source != "saved_archive" || old.FirstRequest != 0 || current.ID != 2 || current.FirstRequest != 1 {
		t.Fatal(old, current)
	}
	if got := s.RecordPrompt("old", nil, 2); got.ID != current.ID {
		t.Fatal("unchanged prompt duplicated", got)
	}
	s.RecordPrompt("new", nil, 3)
	s.RecordPrompt("old", nil, 4)
	data, _ := json.Marshal(s)
	loaded, err := Decode(data)
	if err != nil || len(loaded.Prompts) != 4 || loaded.Prompts[1].Sections[0].End != 3 {
		t.Fatal(loaded, err)
	}
	data = []byte(strings.Replace(string(data), `"end":3`, `"end":999`, 1))
	if _, err := Decode(data); err == nil {
		t.Fatal("accepted invalid section range")
	}
}

func TestPromptMetadataRejectsCorruptionAndLoadsLegacy(t *testing.T) {
	if _, err := Decode([]byte(`[{"role":"system","content":"legacy"}]`)); err == nil {
		t.Fatal(err)
	}
	s := New([]llm.Message{{Role: llm.RoleSystem, Content: "system"}})
	s.RecordPrompt("system", nil, 1)
	s.LastRequest = &RequestManifest{Number: 1, PromptRevision: 1, ArchiveMessages: 1, MessageCount: 1, Included: []MessageRange{{1, 1}}}
	data, _ := json.Marshal(s)
	if _, err := Decode(data); err != nil {
		t.Fatal(err)
	}
	s.LastRequest.Included[0].End = 2
	data, _ = json.Marshal(s)
	if _, err := Decode(data); err == nil {
		t.Fatal("accepted nonexistent archive ID")
	}
	s.LastRequest = nil
	s.Prompts[0].Text = "tampered"
	data, _ = json.Marshal(s)
	if _, err := Decode(data); err == nil {
		t.Fatal("accepted mismatched prompt hash")
	}
}

func TestPromptSectionsFollowCompositionOrder(t *testing.T) {
	s := New(nil)
	p := s.RecordPrompt("core\n\nidentity\n\nidentity", []llm.PromptSection{
		{Name: "core", Text: "core\n\nidentity"},
		{Name: "identity", Text: "\n\nidentity"},
	}, 1)
	if len(p.Sections) != 2 || p.Sections[1].Start != len("core\n\nidentity") {
		t.Fatal("matched identity inside another section", p.Sections)
	}
}
