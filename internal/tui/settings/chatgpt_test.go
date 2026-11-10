package settings

import (
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/config"
)

func TestChatGPTSettingsHideUnsupportedFieldsAndRoundTrip(t *testing.T) {
	p := profile()
	p.Provider = config.Provider{Kind: "chatgpt", URL: "https://api.openai.com/v1", Model: "test-model"}
	for _, section := range []string{"model", "generation"} {
		_, fields := PageFields(p, section, config.Preferences{})
		f := Model{EditSession: &EditSession{Draft: New(p), Section: section}}
		f.Fields = fields
		f.EditSession.Draft.Visit(section, fields)
		for i, field := range fields {
			unsupported := field.Key == "temperature" || field.Key == "max" || field.Key == "key-env" || field.Key == "endpoint"
			if unsupported && f.FieldVisible(i) {
				t.Fatal("unsupported field offered", field.Key)
			}
			if field.Key == "context" && !f.FieldVisible(i) {
				t.Fatal("local context budget hidden")
			}
		}
		f.EditSession.Draft.Store(section, fields)
		got, err := f.EditSession.Draft.Resolve()
		if err != nil || got.Provider != p.Provider || got.Generation != p.Generation {
			t.Fatal("settings round trip changed profile", err)
		}
	}
	if got := SuggestedBackendURL("http://127.0.0.1:9931", "llama", "chatgpt"); got != p.Provider.URL {
		t.Fatal(got)
	}
	f := Model{EditSession: &EditSession{Draft: New(p), Section: "model", AccountLabel: "connected@example.com"}}
	if !strings.Contains(f.note(), "connected@example.com") {
		t.Fatal("account missing")
	}
}
