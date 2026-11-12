package files

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
		{"read", Read{WS: ws}, `{"path":"external/secret.txt"}`},
		{"write", Write{WS: ws}, `{"path":"external/new.txt","content":"bad"}`},
		{"patch", Patch{WS: ws}, `{"path":"external/secret.txt","old_content":"OUTSIDE_SECRET","new_content":"bad"}`},
		{"move source", Move{WS: ws}, `{"from":"external/secret.txt","to":"stolen.txt"}`},
		{"move destination", Move{WS: ws}, `{"from":"local.txt","to":"external/new.txt"}`},
		{"list", List{WS: ws}, `{"path":"external"}`},
		{"grep", Grep{WS: ws}, `{"path":"external","pattern":"SECRET","include_ignored":true}`},
		{"image", Read{WS: ws, Vision: true}, `{"path":"external/shot.png"}`},
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
	if preview := Preview(ws, "patch_file", json.RawMessage(`{"path":"external/secret.txt","old_content":"OUTSIDE_SECRET","new_content":"bad"}`)); preview != "" {
		t.Fatal("preview read through a link", preview)
	}
}
