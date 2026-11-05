package chatcompletions

import "testing"

// ClientFor is the single place the -backend-kind flag becomes a dialect.
// Every kind it advertises must build, and nothing else may.
func TestClientFor(t *testing.T) {
	kinds := Kinds()
	if len(kinds) == 0 {
		t.Fatal("Kinds() is empty")
	}
	for _, k := range kinds {
		base := "http://example.invalid"
		c, err := ClientFor(k, base, "local")
		if err != nil {
			t.Errorf("ClientFor(%q): %v", k, err)
			continue
		}
		if c.Backend() == "" {
			t.Errorf("ClientFor(%q) built a client with no backend name", k)
		}
	}
	if _, err := ClientFor("ollama", "http://example.invalid", "local"); err == nil {
		t.Error("ClientFor accepted an unknown kind")
	}
}
