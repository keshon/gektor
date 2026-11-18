package ssh

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBindingChangesInvalidatePinsWithoutRestart(t *testing.T) {
	server := serveSSH(t)
	service, path := testService(t, server)
	revision := testRevision(t, service, "test")
	request := ExecRequest{Connection: "test", ConnectionRevision: revision, Command: "ok"}
	profile := testProfile(t, service, "test")
	profile.User = "another-user"
	saveTestProfile(t, service, "test", profile)

	_, err := service.Exec(t.Context(), request)
	if err == nil || !strings.Contains(err.Error(), "revision mismatch") || server.calls.Load() != 0 {
		t.Fatal("changed binding used before refresh", err)
	}
	_, err = service.Resource("test", revision, "")
	if err == nil {
		t.Fatal("authorization resource accepted stale revision")
	}
	request.ConnectionRevision = ""
	_, err = service.Exec(t.Context(), request)
	if err == nil {
		t.Fatal("unpinned call accepted")
	}

	connections, err := service.Connections()
	if err != nil || len(connections) != 1 || connections[0].Revision == revision {
		t.Fatal(connections, err)
	}
	raw, err := json.Marshal(connections)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), profile.PasswordEnv) || strings.Contains(string(raw), profile.KnownHosts) {
		t.Fatal("discovery exposed credential or trust references")
	}
	store := Profiles{Path: path}
	binding, err := store.Inspect("test")
	if err != nil || binding != connections[0] {
		t.Fatal(binding, err)
	}
}

func TestNamedRevocationSurvivesProfileReplacement(t *testing.T) {
	server := serveSSH(t)
	service, path := testService(t, server)
	revision := testRevision(t, service, "test")
	profile := testProfile(t, service, "test")
	store := Profiles{Path: path}
	err := store.Revoke("test")
	if err != nil {
		t.Fatal(err)
	}
	err = store.Save(map[string]Profile{})
	if err != nil {
		t.Fatal(err)
	}
	err = store.Save(map[string]Profile{"test": profile})
	if err != nil {
		t.Fatal(err)
	}

	reopened := New(Config{Workspace: service.WS, ProfilesPath: path})
	for _, client := range []*Service{service, reopened} {
		_, err = client.Exec(t.Context(), ExecRequest{Connection: "test", ConnectionRevision: revision, Command: "ok"})
		if err == nil || !strings.Contains(err.Error(), "revoked") {
			t.Fatal(err)
		}
		_, err = client.Transfer(t.Context(), TransferRequest{Connection: "test", ConnectionRevision: revision, LocalPath: "source.txt", RemotePath: "/remote.txt"}, Upload, nil)
		if err == nil || !strings.Contains(err.Error(), "revoked") {
			t.Fatal(err)
		}
		err = client.Check(t.Context(), "test")
		if err == nil || !strings.Contains(err.Error(), "revoked") {
			t.Fatal(err)
		}
		connections, inspectErr := client.Connections()
		if inspectErr != nil || len(connections) != 1 || !connections[0].Revoked {
			t.Fatal(connections, inspectErr)
		}
	}
	if server.calls.Load() != 0 {
		t.Fatal("revocation allowed networking")
	}
}

func TestTransferRechecksBindingAfterLocalAuthorization(t *testing.T) {
	server := serveSSH(t)
	service, _ := testService(t, server)
	request := TransferRequest{Connection: "test", ConnectionRevision: testRevision(t, service, "test"), LocalPath: "source.txt", RemotePath: "/remote.txt"}
	err := service.WS.WriteFile("source.txt", []byte("payload"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string) error {
		profile := testProfile(t, service, "test")
		profile.User = "changed-during-approval"
		saveTestProfile(t, service, "test", profile)
		return nil
	}
	_, err = service.Transfer(t.Context(), request, Upload, authorize)
	if err == nil || !strings.Contains(err.Error(), "revision mismatch") || server.calls.Load() != 0 {
		t.Fatal("transfer used changed binding after approval", err)
	}
}
