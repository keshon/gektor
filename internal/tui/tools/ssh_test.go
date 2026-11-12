package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/ssh"
)

func TestSSHPreferencesPreserveReferencesWithoutResolvingSecrets(t *testing.T) {
	model, _ := toolsFixture(t)
	model.SSHProfiles = ssh.Profiles{Path: filepath.Join(t.TempDir(), "ssh.json")}
	values := map[string]string{"name": "dev", "host": "example.com", "port": "22", "user": "tester", "known_hosts": filepath.Join(t.TempDir(), "known_hosts"), "auth": "password environment", "password_env": "MISSING_SSH_PASSWORD"}
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

	err = model.SaveSSH("", values)
	if err != nil {
		t.Fatal(err)
	}
	err = model.SaveSSH("", values)
	if err == nil {
		t.Fatal("duplicate profile replaced")
	}
	values["auth"], values["private_key"], values["passphrase_env"] = "key file", filepath.Join(t.TempDir(), "id_ed25519"), "SSH_KEY_PASSPHRASE"
	err = model.SaveSSH("dev", values)
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
	err = model.SaveSSH("", map[string]string{"name": "replacement"})
	if err == nil {
		t.Fatal("malformed configuration overwritten")
	}
	saved, err := os.ReadFile(model.SSHProfiles.Path)
	if err != nil || string(saved) != string(original) {
		t.Fatal("original configuration damaged", err)
	}
}
