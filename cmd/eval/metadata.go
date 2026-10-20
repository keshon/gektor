package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/prompts"
)

type evalMetadata struct {
	BackendKind    string `json:"backend_kind"`
	Backend        string `json:"backend"`
	Model          string `json:"model"`
	ContextLimit   int    `json:"context_limit"`
	Jobs           int    `json:"jobs"`
	MaxTokens      int    `json:"max_tokens"`
	Runs           int    `json:"runs"`
	DrySampler     bool   `json:"dry_sampler"`
	Started        string `json:"started"`
	GoVersion      string `json:"go_version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	SourceRevision string `json:"source_revision,omitempty"`
	SourceModified *bool  `json:"source_modified,omitempty"`
	BinarySHA256   string `json:"binary_sha256"`
	PromptsSHA256  string `json:"prompts_sha256"`
	InputsSHA256   string `json:"inputs_sha256"`
}

// writeMeta fails before model execution if the experiment cannot be recorded.
// Auth flags are deliberately absent: metadata must not contain API keys.
func writeMeta(runDir string, meta evalMetadata, probes []Probe) error {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	if meta.BinarySHA256, err = fileDigest(path); err != nil {
		return err
	}
	if meta.InputsSHA256, err = inputsDigest(probes); err != nil {
		return err
	}
	meta.PromptsSHA256 = prompts.Digest()
	meta.Started = time.Now().Format(time.RFC3339)
	meta.GoVersion, meta.OS, meta.Arch = runtime.Version(), runtime.GOOS, runtime.GOARCH
	// This is the source checkout, not a claim about a previously built binary.
	if revision, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		meta.SourceRevision = strings.TrimSpace(string(revision))
		if status, err := exec.Command("git", "status", "--porcelain", "--untracked-files=normal").Output(); err == nil {
			modified := len(status) > 0
			meta.SourceModified = &modified
		}
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644)
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// The model sees the selected probe's task and copied seed. Include all seed
// files, including workspace skills, rather than unrelated repository guides.
func inputsDigest(probes []Probe) (string, error) {
	type input struct {
		Name  string            `json:"name"`
		Probe Probe             `json:"probe"`
		Files map[string]string `json:"files"`
	}
	inputs := make([]input, 0, len(probes))
	for _, probe := range probes {
		item := input{Name: probe.Name, Probe: probe, Files: map[string]string{}}
		if probe.Seed != "" {
			err := filepath.WalkDir(probe.Seed, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(probe.Seed, path)
				if err != nil {
					return err
				}
				digest, err := fileDigest(path)
				if err != nil {
					return err
				}
				item.Files[filepath.ToSlash(rel)] = digest
				return nil
			})
			if err != nil {
				return "", fmt.Errorf("probe %s seed: %w", probe.Name, err)
			}
		}
		inputs = append(inputs, item)
	}
	data, err := json.Marshal(inputs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
