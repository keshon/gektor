package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testWS(t *testing.T) (*Workspace, string) {
	t.Helper()
	// EvalSymlinks because the OS temp dir is itself a symlink on macOS
	// (/var -> /private/var); without it every containment check here
	// would compare two spellings of the same directory.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	ws, err := New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ws, root
}

func TestResolve_AllowsPathsInsideRoot(t *testing.T) {
	ws, root := testWS(t)
	for _, rel := range []string{"a.txt", "sub/a.txt", "./a.txt", "sub/../a.txt", "."} {
		got, err := ws.Resolve(rel)
		if err != nil {
			t.Errorf("Resolve(%q) = error %v, want allowed", rel, err)
			continue
		}
		if got != root && !strings.HasPrefix(got, root+string(filepath.Separator)) {
			t.Errorf("Resolve(%q) = %q, outside root %q", rel, got, root)
		}
	}
}

// The whole point of the type: every file tool routes through Resolve, so
// anything it lets through is somewhere the agent can write.
func TestResolve_RejectsEscapes(t *testing.T) {
	ws, _ := testWS(t)
	escapes := []string{
		"..",
		"../outside.txt",
		"../../outside.txt",
		"sub/../../outside.txt",
		"a/b/../../../outside.txt",
	}
	if runtime.GOOS == "windows" {
		escapes = append(escapes, `..\outside.txt`, `sub\..\..\outside.txt`)
	}
	for _, rel := range escapes {
		if got, err := ws.Resolve(rel); err == nil {
			t.Errorf("Resolve(%q) = %q, want rejected as an escape", rel, got)
		}
	}
}

// A sibling directory whose name merely starts with the root's name is
// not inside the root: /tmp/ws must not admit /tmp/ws-evil. This is why
// the containment test needs the separator and cannot be a bare prefix.
func TestResolve_RejectsSiblingWithRootAsNamePrefix(t *testing.T) {
	ws, root := testWS(t)
	rel := ".." + string(filepath.Separator) + filepath.Base(root) + "-evil"
	if got, err := ws.Resolve(rel); err == nil {
		t.Errorf("Resolve(%q) = %q, want rejected — sibling, not child", rel, got)
	}
}

func TestResolveAcceptsAbsolutePathsOnlyInsideRoot(t *testing.T) {
	ws, root := testWS(t)
	for _, name := range []string{root, filepath.Join(root, "inside")} {
		if got, err := ws.Resolve(name); err != nil || got != name {
			t.Fatalf("Resolve(%q) = %q, %v", name, got, err)
		}
	}
	if _, err := ws.Resolve(filepath.Join(t.TempDir(), "outside")); err == nil {
		t.Fatal("accepted external absolute path")
	}
}

func TestRootedAccessRejectsEscapingSymlink(t *testing.T) {
	ws, root := testWS(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := ws.WriteFile("link/escaped.txt", []byte("bad"), 0600); err == nil {
		t.Fatal("write escaped through symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatal("outside file was created")
	}
}
