package config

import "testing"

func TestResponseCeilingCanExceedDetectedWindow(t *testing.T) {
	p := validProfile()
	p.Generation.MaxTokens, p.Generation.ContextLimit = 8192, 4096
	err := p.Validate()
	if err != nil {
		t.Fatalf("response ceiling should be clamped by the agent: %v", err)
	}
}

func TestCloneCopiesIdentity(t *testing.T) {
	p := validProfile()
	p.Identity = &Identity{UserName: "User"}
	next := p.Clone()
	next.Identity.UserName = "Changed"
	if p.Identity.UserName != "User" {
		t.Fatal("Clone aliased the identity block")
	}
}
