package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/userdirs"
)

func TestSSHCommandsPreserveMultipleConnectionsAndRejectLiteralSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	file := filepath.Join(home, "profile.json")
	profile := ssh.Profile{Host: "example.com", User: "tester", KnownHosts: filepath.Join(home, "known_hosts"), PasswordEnv: "SSH_FIRST"}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(file, raw, 0600)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runSSH([]string{"save", "first", file}, &output)
	if err != nil {
		t.Fatal(err)
	}
	profile.PasswordEnv = "SSH_SECOND"
	raw, err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(file, raw, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = runSSH([]string{"save", "second", file}, &output)
	if err != nil {
		t.Fatal(err)
	}
	err = runSSH([]string{"list"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	var profiles map[string]ssh.Profile
	err = json.Unmarshal(output.Bytes(), &profiles)
	if err != nil || len(profiles) != 2 || profiles["first"].PasswordEnv != "SSH_FIRST" || profiles["second"].PasswordEnv != "SSH_SECOND" {
		t.Fatal(profiles, err)
	}

	err = os.WriteFile(file, []byte(`{"password":"literal"}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = runSSH([]string{"save", "first", file}, &output)
	if err == nil {
		t.Fatal("literal password field accepted")
	}
	err = runSSH([]string{"delete", "second"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	store := ssh.Profiles{Path: userdirs.Sub("ssh.json")}
	profiles, err = store.Load()
	if err != nil || len(profiles) != 1 || profiles["first"].PasswordEnv != "SSH_FIRST" {
		t.Fatal(profiles, err)
	}
}

func TestSSHInspectAndRevokeExposeOnlyBindingMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store := ssh.Profiles{Path: userdirs.Sub("ssh.json")}
	profile := ssh.Profile{Host: "example.com", User: "tester", KnownHosts: filepath.Join(home, "known_hosts"), PasswordEnv: "PRIVATE_SOURCE_NAME"}
	err := store.Save(map[string]ssh.Profile{"test": profile})
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	err = runSSH([]string{"inspect", "test"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	var before ssh.Connection
	err = json.Unmarshal(output.Bytes(), &before)
	if err != nil || len(before.Revision) != 64 || before.Revoked {
		t.Fatal(before, err)
	}
	if bytes.Contains(output.Bytes(), []byte(profile.PasswordEnv)) || bytes.Contains(output.Bytes(), []byte(profile.KnownHosts)) {
		t.Fatal("inspect exposed source or trust-file references")
	}
	output.Reset()
	err = runSSH([]string{"revoke", "test"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	var after ssh.Connection
	err = json.Unmarshal(output.Bytes(), &after)
	if err != nil || !after.Revoked || after.Revision != before.Revision {
		t.Fatal(after, err)
	}
}
