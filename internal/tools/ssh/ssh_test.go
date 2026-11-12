package ssh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sshservice "github.com/keshon/gektor/internal/capabilities/ssh"
)

func TestConnectionsUsesSnapshotWithoutCredentialsOrNetwork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh.json")
	profile := sshservice.Profile{Host: "example.invalid", User: "tester", KnownHosts: filepath.Join(dir, "known_hosts"), PasswordEnv: "NEVER_RESOLVE_THIS"}
	data, err := json.Marshal(map[string]sshservice.Profile{"second": profile, "first": profile})
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, data, 0600)
	if err != nil {
		t.Fatal(err)
	}
	service := sshservice.New(sshservice.Config{ProfilesPath: path})
	err = os.Remove(path)
	if err != nil {
		t.Fatal(err)
	}

	tool := Connections{Service: service}
	out, err := tool.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Connections []sshservice.Connection `json:"connections"`
	}
	err = json.Unmarshal([]byte(out), &result)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Connections) != 2 || result.Connections[0].Name != "first" || result.Connections[0].Port != 22 {
		t.Fatal(out)
	}
	for _, secret := range []string{"NEVER_RESOLVE_THIS", "known_hosts", "password", "private_key"} {
		if strings.Contains(out, secret) {
			t.Fatal("connection metadata exposed credential configuration", out)
		}
	}
}
