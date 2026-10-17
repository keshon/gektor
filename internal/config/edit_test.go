package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// A profile may legitimately carry no Conversation and no Speech, and
// Validate permits both. ApplySection guards the first and then
// dereferenced the second: a staged edit on either block was a crash one
// new caller away.
func TestApplySectionCreatesTheBlockItEdits(t *testing.T) {
	base := Profile{
		Version:    1,
		Provider:   Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"},
		Generation: Generation{MaxTokens: 100, ContextLimit: 1000},
	}

	t.Run("conversation", func(t *testing.T) {
		p := base.Clone()
		if err := ApplySection(&p, "you", map[string]string{"user": "Alex", "persona": "friend"}); err != nil {
			t.Fatal(err)
		}
		if p.Conversation == nil || p.Conversation.UserName != "Alex" || p.Conversation.UserPersona != "friend" {
			t.Fatalf("conversation not created: %+v", p.Conversation)
		}
	})

	t.Run("speech", func(t *testing.T) {
		p := base.Clone()
		if p.Speech != nil {
			t.Fatal("precondition: profile should have no speech")
		}
		values := map[string]string{
			"microphone": "mic", "variant": "nano", "speech-model": "models/nano",
			"seconds": "90", "vad": "silero", "mic-start": "250", "mic-end": "1200",
		}
		if err := ApplySection(&p, "speech", values); err != nil {
			t.Fatal(err)
		}
		if p.Speech == nil {
			t.Fatal("speech block not created")
		}
		// The model directory is stored absolute, so compare on the tail.
		if p.Speech.Microphone != "mic" || p.Speech.MaxSeconds != 90 || !strings.HasSuffix(p.Speech.Speaking.Model, filepath.Join("models", "nano")) {
			t.Fatalf("speech not applied: %+v", p.Speech)
		}
	})
}

// The bundled agent profile has no conversation block at all, so the
// nil case is the ordinary one rather than an edge case.
func TestAgentProfileWithoutConversationIsEditable(t *testing.T) {
	p := Profile{
		Version:    1,
		Provider:   Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"},
		Generation: Generation{MaxTokens: 2048, ContextLimit: 8192},
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := ApplySection(&p, "generation", map[string]string{"temperature": "0.7", "max": "4096", "context": "16384"}); err != nil {
		t.Fatal(err)
	}
	if p.Generation.MaxTokens != 4096 || p.Generation.ContextLimit != 16384 {
		t.Fatalf("generation not applied: %+v", p.Generation)
	}
}