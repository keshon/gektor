package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workspace"
)

// Exercise the shipped executable, not just the workspace loader. No source
// tree or skill files are present beside the executable or in its workspace.
func TestCLIBundledSkillsFromSeparateLaunchDirectory(t *testing.T) {
	install, launch, project := t.TempDir(), t.TempDir(), t.TempDir()
	binary := filepath.Join(install, "gektor.exe")
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, id := range []string{"workflows", "launch-only"} {
		dir := filepath.Join(launch, "skills", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+id+"\ndescription: LAUNCH_DIRECTORY_ONLY\n---\nWRONG_GUIDE"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, msg := range body.Messages {
			if strings.Contains(msg.Content, "LAUNCH_DIRECTORY_ONLY") || strings.Contains(msg.Content, "WRONG_GUIDE") {
				t.Error("launch-directory skill leaked into selected workspace")
			}
		}
		toolCall := func(id, name, args string) map[string]any {
			return map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}
		}
		message := map[string]any{"role": "assistant"}
		finish := "tool_calls"
		switch requests.Add(1) {
		case 1:
			prompt := fmt.Sprint(body.Messages)
			if !strings.Contains(prompt, project) {
				t.Error("agent prompt does not name the selected workspace", project)
			}
			for _, id := range []string{"workflows", "visual-investigation"} {
				if !strings.Contains(prompt, id) {
					t.Error("initial prompt missing bundled skill", id)
				}
			}
			message["tool_calls"] = []any{toolCall("discover", "system_info", `{"group":"skills"}`)}
		case 2:
			found := map[string]bool{}
			for _, msg := range body.Messages {
				if msg.Role != "tool" {
					continue
				}
				var page struct{ Skills []workspace.Skill }
				if err := json.Unmarshal([]byte(msg.Content), &page); err != nil {
					t.Error("skill discovery did not return JSON", err)
				}
				for _, skill := range page.Skills {
					found[skill.ID] = skill.Source == "builtin" && skill.Path == "builtin:"+skill.ID+"/SKILL.md"
				}
			}
			if !found["workflows"] || !found["visual-investigation"] || len(found) != 2 {
				t.Error("unexpected skill discovery from external workspace", found)
			}
			message["tool_calls"] = []any{toolCall("workflow", "load_skill", `{"id":"workflows"}`), toolCall("visual", "load_skill", `{"id":"visual-investigation"}`)}
		default:
			found := map[string]bool{}
			for _, msg := range body.Messages {
				if msg.Role != "tool" {
					continue
				}
				for id, marker := range map[string]string{"workflows": "script_transform", "visual-investigation": "image_attachments"} {
					header, guide, complete := strings.Cut(msg.Content, "\n\n")
					digest := fmt.Sprintf("SHA-256: %x", sha256.Sum256([]byte(guide)))
					if complete && strings.HasPrefix(header, "Skill "+id+"\nSource: builtin:"+id+"/SKILL.md\n") && strings.Contains(header, digest) && strings.Contains(guide, marker) {
						found[id] = true
					}
				}
			}
			if len(found) != 2 {
				t.Error("agent did not receive both complete bundled guides", found)
			}
			message["content"], finish = "Both bundled guides loaded.", "stop"
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}})
	}))
	defer srv.Close()
	cmd := exec.CommandContext(ctx, binary, "-workspace", project, "-backend-kind", "openai", "-backend", srv.URL, "-model", "test", "-context-limit", "32768", "-max-steps", "5", "-yes", "Check bundled skills.")
	cmd.Dir = launch
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CLI from separate launch directory: %v\n%s", err, output)
	}
	if requests.Load() != 3 {
		t.Fatal("agent did not complete discovery and guide loading", requests.Load())
	}
	files, err := os.ReadDir(project)
	if err != nil || len(files) != 0 {
		t.Fatal("bundled skills wrote into the workspace", files, err)
	}
}
