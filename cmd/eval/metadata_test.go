package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInputsDigestTracksTasksAndWorkspaceSkills(t *testing.T) {
	seed := t.TempDir()
	skill := filepath.Join(seed, "skills", "example", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("original instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	probes := []Probe{{Name: "example", Task: "inspect the workspace", Seed: seed}}
	digest := func() string {
		t.Helper()
		got, err := inputsDigest(probes)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	original := digest()
	if digest() != original {
		t.Fatal("unchanged inputs changed identity")
	}
	if err := os.WriteFile(skill, []byte("new instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := digest()
	if changed == original {
		t.Fatal("workspace skill change was invisible")
	}
	probes[0].Task = "a different task"
	if digest() == changed {
		t.Fatal("task change was invisible")
	}
	changed = digest()
	probes[0].Name = "renamed"
	if digest() == changed {
		t.Fatal("probe selection identity was lost")
	}
	changed = digest()
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}
	if digest() == changed {
		t.Fatal("removed seed file was invisible")
	}
}

func TestSourceDigestTracksUncommittedSources(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	before, err := sourceDigest()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "adapter.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := sourceDigest()
	if err != nil || after == before {
		t.Fatal("untracked source omitted", err)
	}
	if err := os.WriteFile(file, []byte("package main\n// changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := sourceDigest()
	if err != nil || changed == after {
		t.Fatal("dirty content omitted", err)
	}
}

func TestMetadataRecordsArtifactAndExperimentSettings(t *testing.T) {
	dir := t.TempDir()
	meta := evalMetadata{BackendKind: "llama", Backend: "http://localhost:8080", Model: "test", ContextLimit: 8192, Jobs: 1, MaxTokens: 2048, Runs: 3, DrySampler: true}
	if err := writeMeta(dir, meta, []Probe{{Name: "test", Task: "task"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got evalMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.MaxTokens != 2048 || got.Runs != 3 || !got.DrySampler || got.Model != "test" || got.Jobs != 1 {
		t.Fatalf("experiment settings lost: %+v", got)
	}
	for _, digest := range []string{got.BinarySHA256, got.PromptsSHA256, got.InputsSHA256} {
		if len(digest) != 64 {
			t.Fatalf("missing SHA-256: %q", digest)
		}
	}
	if got.Started == "" || got.GoVersion == "" || got.OS == "" || got.Arch == "" {
		t.Fatal("runtime identity missing")
	}
	if err := writeMeta(filepath.Join(dir, "missing"), meta, nil); err == nil {
		t.Fatal("failed metadata write was ignored")
	}
}

func TestInputsDigestRejectsUnreadableSeed(t *testing.T) {
	if _, err := inputsDigest([]Probe{{Name: "broken", Seed: filepath.Join(t.TempDir(), "missing")}}); err == nil {
		t.Fatal("missing seed was silently fingerprinted")
	}
}
