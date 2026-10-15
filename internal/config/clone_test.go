package config

import (
	"testing"

	"github.com/keshon/tars/internal/speech"
)

func TestProfileCloneDoesNotShareOptionalSettings(t *testing.T) {
	main, post := true, false
	p := Profile{Conversation: &Conversation{UserName: "User", PreferCharacterPrompt: &main, PreferCharacterPostHistory: &post}, Speech: &speech.Config{Microphone: "original"}}
	c := p.Clone()
	c.Conversation.UserName = "Changed"
	*c.Conversation.PreferCharacterPrompt = false
	*c.Conversation.PreferCharacterPostHistory = true
	c.Speech.Microphone = "changed"
	if p.Conversation.UserName != "User" || !main || post || p.Speech.Microphone != "original" {
		t.Fatal("clone shares mutable optional settings")
	}
}
