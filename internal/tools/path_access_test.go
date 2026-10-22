package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

func TestFileToolsRejectEscapingLinks(t *testing.T) {
	root, outside := filepath.Join(t.TempDir(), "workspace with spaces"), t.TempDir()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("OUTSIDE_SECRET"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "local.txt"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "external")
	if runtime.GOOS == "windows" {
		// Junction creation does not require Developer Mode or symlink privilege.
		if out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
			t.Fatalf("create junction: %v: %s", err, out)
		}
	} else if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	for _, tc := range []struct {
		name string
		tool agent.Tool
		args string
	}{
		{"read", ReadFile{WS: ws}, `{"path":"external/secret.txt"}`},
		{"write", WriteFile{WS: ws}, `{"path":"external/new.txt","content":"bad"}`},
		{"patch", PatchFile{WS: ws}, `{"path":"external/secret.txt","old_content":"OUTSIDE_SECRET","new_content":"bad"}`},
		{"move source", MoveFile{WS: ws}, `{"from":"external/secret.txt","to":"stolen.txt"}`},
		{"move destination", MoveFile{WS: ws}, `{"from":"local.txt","to":"external/new.txt"}`},
		{"list", ListFiles{WS: ws}, `{"path":"external"}`},
		{"grep", GrepFiles{WS: ws}, `{"path":"external","pattern":"SECRET","include_ignored":true}`},
		{"image", ReadFile{WS: ws, Vision: true}, `{"path":"external/shot.png"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.tool.Run(t.Context(), json.RawMessage(tc.args))
			if err == nil || strings.Contains(out, "OUTSIDE_SECRET") {
				t.Fatalf("link was followed: %q, %v", out, err)
			}
		})
	}
	if data, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(data) != "OUTSIDE_SECRET" {
		t.Fatal("outside file changed")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("outside file created")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "local.txt")); string(data) != "local" {
		t.Fatal("failed move removed its source")
	}
	if preview := previewPatch(ws, json.RawMessage(`{"path":"external/secret.txt","old_content":"OUTSIDE_SECRET","new_content":"bad"}`)); preview != "" {
		t.Fatal("preview read through a link", preview)
	}
}

func TestAbsoluteFilePathsShareRelativeAccessAndPermissions(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	path := filepath.Join(root, "calculator.html")
	args, _ := json.Marshal(map[string]string{"path": path, "content": "calculator"})
	if _, err := (WriteFile{WS: ws}).Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if data, err := ws.ReadFile("calculator.html"); err != nil || string(data) != "calculator" {
		t.Fatal("absolute write did not use the workspace", err)
	}
	resources, err := PathResources(ws, "write_file", args)
	if err != nil || !reflect.DeepEqual(resources, []string{"calculator.html"}) {
		t.Fatal("absolute path bypasses relative permission rules", resources, err)
	}
	args, _ = json.Marshal(map[string]string{"from": path, "to": filepath.Join(root, "private", "target.html")})
	resources, err = PathResources(ws, "move_file", args)
	if err != nil || !reflect.DeepEqual(resources, []string{"calculator.html", "private/target.html"}) {
		t.Fatal("move permissions omit a path", resources, err)
	}
	args, _ = json.Marshal(map[string]string{"path": filepath.Join(t.TempDir(), "outside.txt"), "content": "bad"})
	if _, err := (WriteFile{WS: ws}).Run(t.Context(), args); err == nil {
		t.Fatal("accepted external absolute write")
	}
}
