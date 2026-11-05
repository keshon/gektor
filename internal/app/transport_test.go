package app

import "testing"

func TestGEKTORAPIKeyTakesPrecedence(t *testing.T) {
	for _, name := range []string{"GEKTOR_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "DEEPSEEK_API_KEY", "GROQ_API_KEY", "TOGETHER_API_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(name, "")
	}
	t.Setenv("OPENAI_API_KEY", "provider-key")
	if APIKeyFromEnv() != "provider-key" {
		t.Fatal("provider key missing")
	}
	t.Setenv("GEKTOR_API_KEY", "new-key")
	if APIKeyFromEnv() != "new-key" {
		t.Fatal("new key does not take precedence")
	}
}
