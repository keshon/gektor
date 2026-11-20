package workflow

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/credentials"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func registerHTTPConnection(t *testing.T) credentials.Store {
	t.Helper()
	store := credentials.Store{Directory: userdirs.Sub("credentials")}
	err := store.Register("api", credentials.Config{
		Origin: "https://api.example.com",
		Env:    "UNRESOLVED_HTTP_SECRET",
		Header: "Authorization",
		Prefix: "Bearer ",
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestHTTPConnectionPickerPinsRemovesAndUndoesWithoutTouchingArguments(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	store := registerHTTPConnection(t)
	d := workflowRecipe()
	d.Steps[0].Tool = "http_request"
	d.Steps[0].Args = json.RawMessage(`{"url":"https://api.example.com/items","method":"GET","headers":{"Accept":"application/json"}}`)
	w.editor.Replace(d)
	w.graph.Selected = d.Steps[0].ID
	before := w.editor.Source()
	m.workflowCommand("binding")
	values := m.formSnapshot(t).Values()
	values["connection"] = "api / https://api.example.com"
	err := m.applyValues(values)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := store.Inspect("api")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	err = json.Unmarshal(w.editor.Definition.Steps[0].Args, &got)
	if err != nil || string(got["credential"]) != `"api"` || string(got["credential_revision"]) != `"`+binding.Revision+`"` {
		t.Fatal("picker omitted the inspected pin", string(w.editor.Definition.Steps[0].Args), err)
	}
	w.editor.Undo()
	if w.editor.Source() != before {
		t.Fatal("connection selection lost undo")
	}
	w.editor.Redo()
	w.savedConnectionForm()
	values = m.formSnapshot(t).Values()
	values["connection"] = "No credential"
	err = m.applyValues(values)
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	err = json.Unmarshal(w.editor.Definition.Steps[0].Args, &got)
	var want map[string]json.RawMessage
	wantErr := json.Unmarshal(d.Steps[0].Args, &want)
	if err != nil || wantErr != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("removing a credential changed other arguments", got, want, err, wantErr)
	}
}

func TestHTTPConnectionPickerRejectsChangedRevokedOrWrongOrigin(t *testing.T) {
	for _, change := range []string{"changed", "revoked", "origin", "draft", "frozen"} {
		t.Run(change, func(t *testing.T) {
			m := workflowTestModel(t)
			w := m.workflows
			store := registerHTTPConnection(t)
			d := workflowRecipe()
			d.Steps[0].Tool = "http_request"
			d.Steps[0].Args = json.RawMessage(`{"url":"https://api.example.com/items"}`)
			if change == "origin" {
				d.Steps[0].Args = json.RawMessage(`{"url":"https://other.example.com/items"}`)
			}
			w.editor.Replace(d)
			w.graph.Selected = d.Steps[0].ID
			w.savedConnectionForm()
			values := m.formSnapshot(t).Values()
			values["connection"] = "api / https://api.example.com"
			var err error
			switch change {
			case "changed":
				binding, inspectErr := store.Inspect("api")
				if inspectErr != nil {
					t.Fatal(inspectErr)
				}
				err = store.Update("api", binding.Revision, credentials.Config{Origin: binding.Origin, Env: "DIFFERENT_HTTP_SECRET", Header: "Authorization"})
			case "revoked":
				err = store.Revoke("api")
			case "draft":
				d.Name = "Changed"
				w.editor.Replace(d)
			case "frozen":
				w.run = &workflow.Run{}
			}
			if err != nil {
				t.Fatal(err)
			}
			before := w.editor.Source()
			err = m.applyValues(values)
			if err == nil || w.editor.Source() != before {
				t.Fatal("unavailable connection changed the draft", err)
			}
		})
	}
}

func TestSSHConnectionPickerPinsAllExecutableNodesWithoutAuthenticating(t *testing.T) {
	for _, tool := range []string{"ssh_exec", "ssh_upload", "ssh_download"} {
		t.Run(tool, func(t *testing.T) {
			m := workflowTestModel(t)
			w := m.workflows
			store := ssh.Profiles{Path: userdirs.Sub("ssh.json")}
			profiles := map[string]ssh.Profile{"remote": {
				Host:        "does-not-exist.invalid",
				Port:        2222,
				User:        "operator",
				KnownHosts:  filepath.Join(t.TempDir(), "missing-known-hosts"),
				PasswordEnv: "UNRESOLVED_SSH_SECRET",
			}}
			err := store.Save(profiles)
			if err != nil {
				t.Fatal(err)
			}
			d := workflowRecipe()
			d.Steps[0].Tool = tool
			d.Steps[0].Args = json.RawMessage(`{"command":"pwd"}`)
			if tool != "ssh_exec" {
				d.Steps[0].Args = json.RawMessage(`{"local_path":"file.txt","remote_path":"/tmp/file.txt"}`)
			}
			w.editor.Replace(d)
			w.graph.Selected = d.Steps[0].ID
			m.workflowCommand("binding")
			values := m.formSnapshot(t).Values()
			unchanged := w.editor.Source()
			err = m.applyValues(values)
			if err == nil || w.editor.Source() != unchanged {
				t.Fatal("picker selected an SSH destination without a choice", err)
			}
			values["connection"] = "remote / operator@does-not-exist.invalid:2222"
			err = m.applyValues(values)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := store.Inspect("remote")
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]json.RawMessage
			err = json.Unmarshal(w.editor.Definition.Steps[0].Args, &got)
			if err != nil || string(got["connection"]) != `"remote"` || string(got["connection_revision"]) != `"`+binding.Revision+`"` {
				t.Fatal("SSH selection lost the pin", got, err)
			}
			delete(got, "connection")
			delete(got, "connection_revision")
			var want map[string]json.RawMessage
			err = json.Unmarshal(d.Steps[0].Args, &want)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("SSH selection changed operation arguments", got, want, err)
			}
			w.savedConnectionForm()
			values = m.formSnapshot(t).Values()
			err = store.Revoke("remote")
			if err != nil {
				t.Fatal(err)
			}
			before := w.editor.Source()
			err = m.applyValues(values)
			if err == nil || w.editor.Source() != before {
				t.Fatal("revoked SSH connection accepted", err)
			}
		})
	}
}

func TestConnectionPickerRefusesMappedNodesAndOmitsRevokedChoices(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	store := registerHTTPConnection(t)
	err := store.Revoke("api")
	if err != nil {
		t.Fatal(err)
	}
	choices, err := connectionChoices("http_request")
	if err != nil || len(choices) != 0 {
		t.Fatal("revoked HTTP binding offered", choices, err)
	}
	d := workflowRecipe()
	d.Steps[0].Tool = "http_request"
	d.Steps[0].ForEach = json.RawMessage(`[{}]`)
	w.editor.Replace(d)
	w.graph.Selected = d.Steps[0].ID
	before := w.editor.Source()
	m.workflowCommand("binding")
	if m.chrome.DialogID() != (screen.DialogID{}) || w.editor.Source() != before || !strings.Contains(w.notice, "each item's arguments") {
		t.Fatal("picker silently changed mapped arguments", w.notice)
	}
}
