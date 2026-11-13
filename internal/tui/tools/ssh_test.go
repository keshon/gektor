package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/credentials"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestSSHPreferencesPreserveReferencesWithoutResolvingSecrets(t *testing.T) {
	model, _ := toolsFixture(t)
	model.SSHProfiles = ssh.Profiles{Path: filepath.Join(t.TempDir(), "ssh.json")}
	values := map[string]string{"name": "dev", "host": "example.com", "port": "22", "user": "tester", "known_hosts": filepath.Join(t.TempDir(), "known_hosts"), "auth": "Password", "advanced": "Yes", "password_source": "Environment variable", "password_env": "MISSING_SSH_PASSWORD"}
	view := model.SSH(true)
	if len(view.Table.Model.Rows) != 0 || !view.Table.Actions[0].Enabled {
		t.Fatal("empty setup cannot add a connection")
	}
	_, err := model.SSHPreferences("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(model.SSHProfiles.Path)
	if !os.IsNotExist(err) {
		t.Fatal("opening editor saved a file", err)
	}

	settings, parseErr := model.SSHSettings("", values)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	err = model.SaveSSH(settings)
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.SSHSettings("", values)
	if err == nil {
		t.Fatal("duplicate profile replaced")
	}
	values["passphrase_source"] = "Environment variable"
	values["auth"], values["private_key"], values["passphrase_env"] = "Private key", filepath.Join(t.TempDir(), "id_ed25519"), "SSH_KEY_PASSPHRASE"
	settings, err = model.SSHSettings("dev", values)
	if err == nil {
		err = model.SaveSSH(settings)
	}
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := model.SSHProfiles.Load()
	if err != nil || profiles["dev"].PrivateKey == "" || profiles["dev"].PasswordEnv != "" || profiles["dev"].PassphraseEnv != "SSH_KEY_PASSPHRASE" {
		t.Fatal(profiles, err)
	}

	err = model.DeleteSSH("dev", "wrong")
	if err == nil {
		t.Fatal("deletion not confirmed")
	}
	profiles, err = model.SSHProfiles.Load()
	if err != nil || len(profiles) != 1 {
		t.Fatal(profiles, err)
	}
	err = model.DeleteSSH("dev", "dev")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err = model.SSHProfiles.Load()
	if err != nil || len(profiles) != 0 {
		t.Fatal(profiles, err)
	}
}

func TestSSHMalformedConfigurationCannotBeOverwritten(t *testing.T) {
	model, _ := toolsFixture(t)
	model.SSHProfiles = ssh.Profiles{Path: filepath.Join(t.TempDir(), "ssh.json")}
	original := []byte(`{"bad": {"password": "never-show-this"}}`)
	err := os.WriteFile(model.SSHProfiles.Path, original, 0600)
	if err != nil {
		t.Fatal(err)
	}
	view := model.SSH(true)
	if !strings.Contains(view.Table.Description, "Configuration error") || strings.Contains(view.Table.Description, "never-show-this") {
		t.Fatal(view.Table.Description)
	}
	for _, action := range view.Table.Actions {
		if action.Enabled && action.ID != "operations" {
			t.Fatal("malformed configuration allowed action", action)
		}
	}
	_, err = model.SSHSettings("", map[string]string{"name": "replacement"})
	if err == nil {
		t.Fatal("malformed configuration overwritten")
	}
	saved, err := os.ReadFile(model.SSHProfiles.Path)
	if err != nil || string(saved) != string(original) {
		t.Fatal("original configuration damaged", err)
	}
}

func TestSSHFormOnlyShowsRelevantAuthenticationFields(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("default password entry requires encrypted storage")
	}
	model, _ := toolsFixture(t)
	model.SSHProfiles = ssh.Profiles{Path: filepath.Join(t.TempDir(), "ssh.json")}
	request, err := model.SSHPreferences("")
	if err != nil {
		t.Fatal(err)
	}
	controls := &screen.Controls{}
	controls.PresentForm(request)
	draft := controls.Dialog.Form
	visible := func(key string) bool {
		for i, field := range draft.Fields {
			if field.Key == key {
				return draft.FieldVisible(i)
			}
		}
		return false
	}
	if !visible("port") || !visible("auth") || visible("password") || visible("private_key") || visible("password_env") || visible("password_connection") || visible("passphrase") || visible("known_hosts") {
		t.Fatal("normal password form exposes irrelevant fields")
	}
	for i := range draft.Fields {
		if draft.Fields[i].Key == "auth" {
			draft.Fields[i].Value = sshPrivateKey
		}
	}
	if visible("password") || !visible("private_key") || !visible("passphrase") {
		t.Fatal("key authentication did not replace password fields")
	}
	for i := range draft.Fields {
		if draft.Fields[i].Key == "advanced" {
			draft.Fields[i].Value = "Yes"
		}
		if draft.Fields[i].Key == "passphrase_source" {
			draft.Fields[i].Value = sshEnvironment
		}
	}
	if !visible("passphrase_env") || visible("password_env") || visible("passphrase_connection") || visible("passphrase") {
		t.Fatal("advanced key source exposes unrelated fields")
	}
}

func TestSSHNormalEditingPreservesAndChangesSavedCredentialsWithoutIDs(t *testing.T) {
	model, _ := toolsFixture(t)
	model.SSHProfiles = ssh.Profiles{Path: filepath.Join(t.TempDir(), "ssh.json")}
	known := filepath.Join(t.TempDir(), "known_hosts")
	ref := strings.Repeat("a", 32)
	err := model.SSHProfiles.Save(map[string]ssh.Profile{"saved": {Host: "example.com", User: "tester", KnownHosts: known, PasswordRef: ref}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := model.SSHPreferences("saved")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, field := range request.Fields {
		values[field.Key] = field.Value
	}
	if values["password"] != sshKeepPassword || values["advanced"] != "No" {
		t.Fatal("saved credential has no plain keep state")
	}
	if _, exists := values["password_ref"]; exists {
		t.Fatal("raw reference editor remains")
	}
	change, err := model.SSHSettings("saved", values)
	if err != nil || change.Profile.PasswordRef != ref || change.NewSecret != "" {
		t.Fatal("editing requires the ID or replaces existing password", err)
	}
	values["password"] = sshChangePassword
	change, err = model.SSHSettings("saved", values)
	if credentials.Encrypted() && (err != nil || change.NewSecret != ssh.PasswordSecret) {
		t.Fatal("Change did not request secure entry", err)
	}
	values["advanced"], values["password_source"], values["password_connection"] = "Yes", sshSharedCredential, "saved"
	change, err = model.SSHSettings("saved", values)
	if err != nil || change.Profile.PasswordRef != ref {
		t.Fatal("named connection sharing lost reference", err)
	}
}

func TestSSHCollapsingAdvancedPreservesExistingEnvironmentAuthentication(t *testing.T) {
	model, _ := toolsFixture(t)
	model.SSHProfiles = ssh.Profiles{Path: filepath.Join(t.TempDir(), "ssh.json")}
	p := ssh.Profile{Host: "example.com", User: "tester", KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), PrivateKey: filepath.Join(t.TempDir(), "key"), PassphraseEnv: "SSH_KEY_PASSPHRASE"}
	err := model.SSHProfiles.Save(map[string]ssh.Profile{"key": p})
	if err != nil {
		t.Fatal(err)
	}
	request, err := model.SSHPreferences("key")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, field := range request.Fields {
		values[field.Key] = field.Value
	}
	values["advanced"] = "No"
	change, err := model.SSHSettings("key", values)
	if err != nil || change.Profile.PassphraseEnv != p.PassphraseEnv || change.NewSecret != "" {
		t.Fatal("collapsing Advanced changed authentication", err)
	}
	values["passphrase"] = sshNoPassphrase
	change, err = model.SSHSettings("key", values)
	if err != nil || change.Profile.PassphraseEnv != "" || change.Profile.PassphraseRef != "" {
		t.Fatal("explicit passphrase removal failed", err)
	}
}

func TestSSHCheckShowsResultAndOnlyIncludesErrorsOnFailure(t *testing.T) {
	result := SSHCheckResult{name: "dev"}
	dialog := result.Dialog()
	if len(dialog.Lines) != 1 || dialog.Lines[0] != "Connection OK." {
		t.Fatal("unexpected success text", dialog.Lines)
	}
	result.err = errors.New("authentication failed")
	dialog = result.Dialog()
	if len(dialog.Lines) != 3 || dialog.Lines[0] != "Connection failed." || dialog.Lines[2] != "authentication failed" {
		t.Fatal("failure lost its reason", dialog.Lines)
	}
}
