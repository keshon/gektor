package character

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCardsAndPlaceholders(t *testing.T) {
	for _, data := range []string{`{"name":"Ada","description":"Talking to {{user}}","first_mes":"Hello {{user}}","system_prompt":"{{original}} You are {{char}}."}`, `{"spec":"chara_card_v2","spec_version":"2.0","data":{"name":"Ada","description":"Talking to {{user}}","first_mes":"Hello {{user}}","system_prompt":"{{original}} You are {{char}}.","extensions":{"retained_on_disk":true}}}`} {
		path := filepath.Join(t.TempDir(), "card.json")
		os.WriteFile(path, []byte(data), 0600)
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		prompt := c.Instruction("Alex", "Base rules.")
		for _, want := range []string{"Base rules. You are Ada.", "Talking to Alex", "Hello Alex"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("missing %q: %s", want, prompt)
			}
		}
		original, _ := os.ReadFile(path)
		if string(original) != data {
			t.Fatal("import altered source card")
		}
	}
}
func TestUnsupportedCardsFailExplicitly(t *testing.T) {
	for _, data := range []string{`{"spec":"chara_card_v3","data":{"name":"Ada"}}`, `{"name":"Ada","character_book":{"entries":[{"content":"Some lore"}]}}`, `{}`, strings.Repeat(" ", 65537)} {
		path := filepath.Join(t.TempDir(), "card.json")
		os.WriteFile(path, []byte(data), 0600)
		if _, err := Load(path); err == nil {
			t.Fatal("invalid card accepted")
		}
	}
}

func TestCompileMainAndPostHistoryPreferences(t *testing.T) {
	c := Card{Name: "Ada", SystemPrompt: "{{original}} Speak as {{char}}.", PostHistory: "Address {{user}} warmly."}
	main, post := c.Compile("Alex", "Base", true, true)
	if !strings.Contains(main, "Base Speak as Ada.") || post != "Address Alex warmly." {
		t.Fatal(main, post)
	}
	main, post = c.Compile("Alex", "Base", false, false)
	if strings.Contains(main, "Speak as") || post != "" {
		t.Fatal(main, post)
	}
	path := filepath.Join(t.TempDir(), "post.json")
	os.WriteFile(path, []byte(`{"name":"Ada","post_history_instructions":"Answer briefly."}`), 0600)
	if card, err := Load(path); err != nil || card.PostHistory != "Answer briefly." {
		t.Fatal(card, err)
	}
}

func TestEmptyLorebookAndBasePostFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "card.json")
	os.WriteFile(path, []byte(`{"name":"Ada","character_book":{"entries":[]},"post_history_instructions":"{{original}} Be warm."}`), 0600)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_, post := c.Compile("Alex", "Base", true, true, "Be brief.")
	if post != "Be brief. Be warm." {
		t.Fatal(post)
	}
	_, post = c.Compile("Alex", "Base", true, false, "Be brief.")
	if post != "Be brief." {
		t.Fatal(post)
	}
}

func TestBasePostHistoryMacros(t *testing.T) {
	c := Card{Name: "Ada", PostHistory: "{{original}} Continue."}
	_, post := c.Compile("Alex", "Base", true, false, "Speak as {{char}} to {{user}}.")
	if post != "Speak as Ada to Alex." {
		t.Fatalf("base post macros=%s", post)
	}
	_, post = c.Compile("Alex", "Base", true, true, "Speak as {{char}} to {{user}}.")
	if post != "Speak as Ada to Alex. Continue." {
		t.Fatalf("nested base macros=%s", post)
	}
}
