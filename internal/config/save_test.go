package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveCopyPortableAndNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "models", "nested", "weights.gguf")
	p := Profile{Version: 1, Provider: Provider{Kind: "llama", Ownership: "managed", URL: "http://127.0.0.1:19080", Model: "local", Runtime: filepath.Join(root, "runtime", "llama-server.exe"), ModelFile: model}, Generation: Generation{MaxTokens: 100, ContextLimit: 1000}, Conversation: &Conversation{Character: filepath.Join(root, "cards", "character.json"), UserName: "User"}}
	original := p
	card := *p.Conversation
	path := filepath.Join(root, "profiles", "saved.json")
	if err := SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Provider.ModelFile != model || loaded.Conversation.Character != card.Character {
		t.Fatal("saved paths changed target")
	}
	if !reflect.DeepEqual(p, original) || *p.Conversation != card {
		t.Fatal("save mutated source profile")
	}
	before, _ := os.ReadFile(path)
	if SaveCopy(p, path) == nil {
		t.Fatal("overwrote profile")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("existing profile changed")
	}
}
