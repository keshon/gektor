package app

import (
	"testing"

	"github.com/keshon/tars/internal/config"
)

func TestServiceConfigurationsOwnTheirProfiles(t *testing.T) {
	for _, mode := range []string{"chat", "agent"} {
		t.Run(mode, func(t *testing.T) {
			prefer := true
			p := config.Profile{Version: 1,
				Provider:     config.Provider{Kind: "llama", URL: "http://localhost:8080", Model: "local", Ownership: "external"},
				Generation:   config.Generation{MaxTokens: 100, ContextLimit: 1000},
				Conversation: &config.Conversation{UserName: "User", PreferCharacterPrompt: &prefer},
			}
			var services Services
			var err error
			if mode == "chat" {
				services, err = ConfigureChat(p, "", t.TempDir())
			} else {
				services, err = ConfigureAgent(p, t.TempDir())
			}
			if err != nil {
				t.Fatal(err)
			}
			p.Conversation.UserName = "Changed outside"
			prefer = false
			if services.Profile.Conversation.UserName != "User" || !*services.Profile.Conversation.PreferCharacterPrompt {
				t.Fatal("frontend configuration shares caller-owned profile")
			}
			next := services.Profile.Clone()
			next.Conversation.UserName = "Next"
			updated, err := services.Configure(next)
			if err != nil || updated.Profile.Conversation.UserName != "Next" || services.Profile.Conversation.UserName != "User" {
				t.Fatal("reconfiguration mutated current services", err)
			}
		})
	}
}
