package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilesSaveValidatesAndPreservesReferences(t *testing.T) {
	store := Profiles{Path: filepath.Join(t.TempDir(), "ssh.json")}
	profiles, err := store.Load()
	if err != nil || len(profiles) != 0 {
		t.Fatal(profiles, err)
	}
	p := Profile{Host: "example.com", User: "user", KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), PasswordEnv: "SSH_PASSWORD"}
	err = store.Save(map[string]Profile{"host": p})
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	profiles, err = store.Load()
	if err != nil || profiles["host"].PasswordEnv != "SSH_PASSWORD" || profiles["host"].Port != 22 {
		t.Fatal(profiles, err)
	}

	for _, bad := range []Profile{
		{Host: "example.com", User: "user", KnownHosts: p.KnownHosts, PasswordEnv: "literal secret with spaces"},
		{Host: "example.com", User: "user", KnownHosts: "relative", PasswordEnv: "SSH_PASSWORD"},
		{Host: "example.com", User: "user", KnownHosts: p.KnownHosts, PasswordEnv: "SSH_PASSWORD", PrivateKey: p.KnownHosts},
		{Host: strings.Repeat("a", 65536), User: "user", KnownHosts: p.KnownHosts, PasswordEnv: "SSH_PASSWORD"},
	} {
		err = store.Save(map[string]Profile{"host": bad})
		if err == nil {
			t.Fatal("invalid profiles saved")
		}
		saved, readErr := os.ReadFile(store.Path)
		if readErr != nil || string(saved) != string(original) {
			t.Fatal("failed save damaged profiles", readErr)
		}
	}
	err = os.WriteFile(store.Path, []byte(`{"host":{"password":"secret"}}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Load()
	if err == nil {
		t.Fatal("literal password field accepted")
	}
}
