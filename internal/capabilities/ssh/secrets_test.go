package ssh

import (
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/credentials"
	gossh "golang.org/x/crypto/ssh"
)

func TestSavedSSHSecretsAuthenticateAndRemainIndependent(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("encrypted SSH storage requires Windows DPAPI")
	}
	server := serveSSH(t)
	original, path := testService(t, server)
	store := Profiles{Path: path, Credentials: credentials.Store{Directory: filepath.Join(t.TempDir(), "secrets")}}
	p := original.profiles["test"]
	err := store.SaveWithSecret("password", p, PasswordSecret, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	err = store.SaveWithSecret("second", p, PasswordSecret, "other-private-secret")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := store.Load()
	if err != nil || profiles["password"].PasswordRef == profiles["second"].PasswordRef || profiles["password"].PasswordEnv != "" {
		t.Fatal("saved password references are not independent", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), "test-secret") || strings.Contains(string(raw), "other-private-secret") {
		t.Fatal("profile contains secret values", err)
	}
	service := New(Config{Workspace: original.WS, ProfilesPath: path, Credentials: store.Credentials})
	err = service.Check(t.Context(), "password")
	if err != nil {
		t.Fatal("saved password authentication", err)
	}
	execution, err := service.Exec(t.Context(), ExecRequest{Connection: "password", Command: "ok"})
	if err != nil || strings.Contains(execution.Stdout, "test-secret") {
		t.Fatal(err)
	}

	block, err := gossh.MarshalPrivateKeyWithPassphrase(server.private, "test", []byte("saved-key-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "id_ed25519")
	err = os.WriteFile(key, pem.EncodeToMemory(block), 0600)
	if err != nil {
		t.Fatal(err)
	}
	p.PasswordEnv, p.PrivateKey = "", key
	err = store.SaveWithSecret("key", p, PassphraseSecret, "saved-key-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	service = New(Config{Workspace: original.WS, ProfilesPath: path, Credentials: store.Credentials})
	err = service.Check(t.Context(), "key")
	if err != nil {
		t.Fatal("saved passphrase authentication", err)
	}
	profiles, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolveSecret(store.Credentials, "", profiles["key"].PassphraseRef, PasswordSecret)
	if err == nil {
		t.Fatal("passphrase reference accepted as password")
	}
	err = store.Credentials.Delete(profiles["password"].PasswordRef)
	if err != nil {
		t.Fatal(err)
	}
	err = service.Check(t.Context(), "password")
	if err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatal("deleted credential resolved or value leaked", err)
	}
}

func TestSSHSecretSourcesRejectConflictsWithoutReadingStore(t *testing.T) {
	p := Profile{Host: "example.com", User: "tester", KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), PasswordEnv: "SSH_PASSWORD", PasswordRef: strings.Repeat("a", 32)}
	err := ValidateProfile("host", p)
	if err == nil {
		t.Fatal("two password sources accepted")
	}
	p.PasswordEnv = ""
	p.PasswordRef = "../outside"
	err = ValidateProfile("host", p)
	if err == nil {
		t.Fatal("invalid reference accepted")
	}
	p.PasswordRef = strings.Repeat("a", 32)
	p.PassphraseRef = strings.Repeat("b", 32)
	err = ValidateProfile("host", p)
	if err == nil {
		t.Fatal("passphrase without key accepted")
	}
}

func TestFailedSSHSecretSaveDoesNotPublishCredentials(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("encrypted SSH storage requires Windows DPAPI")
	}
	directory := t.TempDir()
	store := Profiles{Path: filepath.Join(directory, "ssh.json"), Credentials: credentials.Store{Directory: filepath.Join(directory, "secrets")}}
	err := store.Save(map[string]Profile{})
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	// The individual profile validates, but publishing the complete file exceeds its size limit.
	p := Profile{Host: strings.Repeat("a", 64<<10), User: "tester", KnownHosts: filepath.Join(directory, "known_hosts"), PasswordEnv: "SSH_PASSWORD"}
	err = store.SaveWithSecret("host", p, PasswordSecret, "private-password")
	if err == nil {
		t.Fatal("oversized profile save succeeded")
	}
	entries, err := os.ReadDir(store.Credentials.Directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed publication retained a credential", err)
	}
	saved, err := os.ReadFile(store.Path)
	if err != nil || string(saved) != string(original) {
		t.Fatal("failed publication damaged profiles", err)
	}
}

func TestSSHRefusesUnencryptedSecretStorage(t *testing.T) {
	if credentials.Encrypted() {
		t.Skip("requires a platform without encrypted storage")
	}
	dir := t.TempDir()
	store := Profiles{Path: filepath.Join(dir, "ssh.json"), Credentials: credentials.Store{Directory: filepath.Join(dir, "secrets")}}
	err := store.SaveWithSecret("host", Profile{}, PasswordSecret, "private-value")
	if err == nil {
		t.Fatal("unencrypted secret storage accepted")
	}
	_, err = os.Stat(store.Credentials.Directory)
	if !os.IsNotExist(err) {
		t.Fatal("unencrypted credential file created", err)
	}
}
