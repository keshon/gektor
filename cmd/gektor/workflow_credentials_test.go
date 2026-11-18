package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow/credentials"
)

func TestWorkflowCredentialUpdateRequiresRevisionAndReturnsNewPin(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	err := workflowCredentials([]string{"register-credential", "-origin", "https://api.example.com", "-env", "UPDATE_SOURCE", "api"}, strings.NewReader(""), &output, directory)
	if err != nil {
		t.Fatal(err)
	}
	var before credentials.Binding
	err = json.Unmarshal(output.Bytes(), &before)
	if err != nil {
		t.Fatal(err)
	}

	args := []string{"update-credential", "-revision", before.Revision, "-origin", before.Origin, "-secret-stdin", "api"}
	output.Reset()
	err = workflowCredentials(args, strings.NewReader("replacement-private-value\n"), &output, directory)
	if err != nil || strings.Contains(output.String(), "replacement-private-value") {
		t.Fatal("update failed or leaked secret", err)
	}
	var after credentials.Binding
	err = json.Unmarshal(output.Bytes(), &after)
	if err != nil || after.Revision == before.Revision || after.Source != "stored" {
		t.Fatal(after, err)
	}

	output.Reset()
	err = workflowCredentials(args, strings.NewReader("rejected-private-value\n"), &output, directory)
	if err == nil || output.Len() != 0 {
		t.Fatal("stale revision accepted", err)
	}
	err = workflowCredentials([]string{"update-credential", "-origin", before.Origin, "-env", "UPDATE_SOURCE", "api"}, strings.NewReader(""), &output, directory)
	if err == nil {
		t.Fatal("update without revision accepted")
	}
}

func TestWorkflowCredentialCommandsDoNotExposeSecrets(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	value := "operator-private-token"
	err := workflowCredentials([]string{"register-credential", "-origin", "https://api.example.com", "-secret-stdin", "api"}, strings.NewReader(value+"\r\n"), &output, directory)
	if err != nil || strings.Contains(output.String(), value) {
		t.Fatal("registration failed or leaked", err)
	}
	var binding credentials.Binding
	err = json.Unmarshal(output.Bytes(), &binding)
	if err != nil || len(binding.Revision) != 64 || binding.Source != "stored" || binding.Revoked {
		t.Fatal("invalid metadata", err)
	}
	store := credentials.Store{Directory: directory}
	_, _, resolved, err := store.Resolve("api", binding.Origin, binding.Revision)
	if err != nil || resolved != value {
		t.Fatal("stdin secret not stored", err)
	}

	output.Reset()
	err = workflowCredentials([]string{"inspect-credential", "api"}, strings.NewReader(""), &output, directory)
	if err != nil || strings.Contains(output.String(), value) || !strings.Contains(output.String(), binding.Revision) {
		t.Fatal("inspection failed or leaked", err)
	}
	output.Reset()
	err = workflowCredentials([]string{"revoke-credential", "api"}, strings.NewReader(""), &output, directory)
	if err != nil || !strings.Contains(output.String(), `"revoked":true`) {
		t.Fatal("revocation failed", err)
	}
	_, _, _, err = store.Resolve("api", binding.Origin, binding.Revision)
	if err == nil {
		t.Fatal("revoked credential resolved")
	}

	for _, args := range [][]string{
		{"register-credential", "-origin", binding.Origin, "-secret-stdin", "-env", "TOKEN", "conflict"},
		{"inspect-credential", "../outside"},
		{"revoke-credential"},
	} {
		output.Reset()
		err = workflowCredentials(args, strings.NewReader(value), &output, directory)
		if err == nil || strings.Contains(err.Error(), value) || strings.Contains(output.String(), value) {
			t.Fatal("bad command accepted or leaked", err)
		}
	}
}
