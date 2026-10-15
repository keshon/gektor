package app

import (
	"errors"
	"testing"

	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
)

type ownedClient struct {
	llm.Client
	closed int
}

func (c *ownedClient) Close() { c.closed++ }

type ownedVoice struct {
	speech.Service
	closed int
}

func (v *ownedVoice) Close() { v.closed++ }

func TestRuntimePersistsBeforeReplacingServices(t *testing.T) {
	old := &ownedClient{}
	voice := &ownedVoice{}
	var candidates []*ownedClient
	p := config.Profile{Provider: config.Provider{Model: "old"}, Speech: &speech.Config{Microphone: "old"}}
	services := Services{Client: old, Profile: p}
	services.Configure = func(p config.Profile) (Services, error) {
		client := &ownedClient{}
		candidates = append(candidates, client)
		return Services{Client: client, Profile: p, Settings: chat.Config{MaxTokens: 100, ContextLimit: 1000}}, nil
	}
	r := NewRuntime(&services)
	r.Voice = voice
	next := p.Clone()
	next.Provider.Model = "new"
	next.Speech.Microphone = "new"
	failure := errors.New("save failed")
	_, err := r.Apply(next, nil, func(Services, *chat.Conversation) error {
		if old.closed != 0 || voice.closed != 0 || r.Services.Client != old {
			t.Fatal("services replaced before persistence")
		}
		return failure
	})
	if !errors.Is(err, failure) || candidates[0].closed != 1 || old.closed != 0 || voice.closed != 0 || r.Services.Client != old {
		t.Fatal("failed apply changed ownership", err)
	}
	conversation, err := r.Apply(next, nil, func(Services, *chat.Conversation) error { return nil })
	if err != nil || conversation != nil || r.Services.Client != candidates[1] || old.closed != 1 || voice.closed != 1 || candidates[1].closed != 0 {
		t.Fatal("successful apply failed to transfer ownership", err)
	}
	r.Close()
	r.Close()
	if candidates[1].closed != 1 || voice.closed != 1 {
		t.Fatal("Close did not release each resource exactly once")
	}
	if _, err := r.Apply(next, nil, nil); err == nil {
		t.Fatal("closed runtime reconfigured")
	}
}

func TestRuntimeSpeechOnlyApplyKeepsAgentClient(t *testing.T) {
	client := &ownedClient{}
	services := Services{Client: client, Configure: func(p config.Profile) (Services, error) { return Services{Profile: p}, nil }}
	r := NewRuntime(&services)
	if _, err := r.Apply(config.Profile{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if r.Services.Client != client || client.closed != 0 {
		t.Fatal("speech-only apply replaced Agent thinking client")
	}
	r.Close()
	if client.closed != 1 {
		t.Fatal("runtime did not own the retained client")
	}
}
