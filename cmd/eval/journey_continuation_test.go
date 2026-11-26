package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

const journeyRepairSteps = 24
const journeyContinuationNote = "Continue the interrupted ledger repair. Finish remaining checks and report the result; do not add features."

type journeyContinuation struct {
	SourceUnchanged     *bool             `json:"source_unchanged_after_attempt,omitempty"`
	SourceDirectory     string            `json:"source_directory"`
	ArchiveSHA256       string            `json:"archive_sha256"`
	PriorEvidenceSHA256 string            `json:"prior_evidence_sha256"`
	WorkspaceSHA256     string            `json:"workspace_sha256"`
	WorkspaceEntries    map[string]string `json:"workspace_entries"`
	PriorRepairOutcome  string            `json:"prior_repair_outcome"`
	PriorRepairSteps    int               `json:"prior_repair_steps"`
	PriorRepairMaxSteps int               `json:"prior_repair_max_steps"`
	AdditionalMaxSteps  int               `json:"additional_repair_max_steps"`
	CombinedMaxSteps    int               `json:"combined_repair_max_steps"`
	Note                string            `json:"continuation_note"`
}

// A continuation gets its own workspace and archive; the source remains evidence
// of the failed attempt. Hashes describe the copied input, not the original seed.
func prepareJourneyContinuation(from, dir string) (*journeyContinuation, []llm.Message, error) {
	source, err := filepath.EvalSymlinks(from)
	if err != nil {
		return nil, nil, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return nil, nil, err
	}
	destination, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, nil, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return nil, nil, err
	}
	rel, err := filepath.Rel(source, destination)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, nil, fmt.Errorf("continuation output must be outside its source directory")
	}
	archive, err := os.ReadFile(filepath.Join(source, "repair.state.json"))
	if err != nil {
		return nil, nil, err
	}
	var snapshot conversation.Snapshot
	err = json.Unmarshal(archive, &snapshot)
	if err != nil {
		return nil, nil, err
	}
	if len(snapshot.History) == 0 {
		return nil, nil, fmt.Errorf("continuation needs a nonempty repair archive")
	}
	prior, err := os.ReadFile(filepath.Join(source, "journey.json"))
	if err != nil {
		return nil, nil, err
	}
	var previous struct {
		Repair       agent.RunReport      `json:"repair"`
		Passed       bool                 `json:"passed"`
		Continuation *journeyContinuation `json:"diagnostic_continuation"`
	}
	err = json.Unmarshal(prior, &previous)
	if err != nil {
		return nil, nil, err
	}
	if previous.Passed || previous.Repair.Outcome == "completed" || previous.Repair.MaxSteps != journeyRepairSteps || previous.Continuation != nil {
		return nil, nil, fmt.Errorf("diagnostic continuation requires an unsuccessful first repair attempt with a %d-step budget", journeyRepairSteps)
	}
	entries, digest, err := journeyWorkspaceDigest(filepath.Join(source, "workspace"))
	if err != nil {
		return nil, nil, err
	}
	work := filepath.Join(destination, "workspace")
	err = os.Mkdir(work, 0700)
	if err != nil {
		return nil, nil, err
	}
	err = copyTree(filepath.Join(source, "workspace"), work)
	if err != nil {
		return nil, nil, err
	}
	_, copiedDigest, err := journeyWorkspaceDigest(work)
	if err != nil {
		return nil, nil, err
	}
	_, unchangedDigest, err := journeyWorkspaceDigest(filepath.Join(source, "workspace"))
	if err != nil {
		return nil, nil, err
	}
	if digest != copiedDigest || digest != unchangedDigest {
		return nil, nil, fmt.Errorf("continuation workspace changed during capture")
	}
	file, err := os.OpenFile(filepath.Join(destination, "repair.state.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, nil, err
	}
	_, err = file.Write(archive)
	closeErr := file.Close()
	if err != nil {
		return nil, nil, err
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	provenance := &journeyContinuation{
		SourceDirectory: source, ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(archive)),
		PriorEvidenceSHA256: fmt.Sprintf("%x", sha256.Sum256(prior)),
		WorkspaceSHA256:     digest, WorkspaceEntries: entries,
		PriorRepairOutcome: previous.Repair.Outcome, PriorRepairSteps: previous.Repair.Steps,
		PriorRepairMaxSteps: previous.Repair.MaxSteps, AdditionalMaxSteps: journeyRepairSteps,
		CombinedMaxSteps: previous.Repair.MaxSteps + journeyRepairSteps,
		Note:             journeyContinuationNote + "\nThe current workspace is the copied root " + work + ". Use this root for all operations; absolute paths in earlier messages refer to the previous workspace. Do not modify the original evidence directory " + source + ".",
	}
	return provenance, snapshot.History, nil
}

func verifyJourneyContinuationSource(provenance *journeyContinuation) error {
	for name, expected := range map[string]string{
		"repair.state.json": provenance.ArchiveSHA256,
		"journey.json":      provenance.PriorEvidenceSHA256,
	} {
		digest, err := fileDigest(filepath.Join(provenance.SourceDirectory, name))
		if err != nil {
			return err
		}
		if digest != expected {
			return fmt.Errorf("source evidence changed during continuation: %s", name)
		}
	}
	_, digest, err := journeyWorkspaceDigest(filepath.Join(provenance.SourceDirectory, "workspace"))
	if err != nil {
		return err
	}
	if digest != provenance.WorkspaceSHA256 {
		return fmt.Errorf("source workspace changed during continuation")
	}
	return nil
}

func journeyWorkspaceDigest(root string) (map[string]string, string, error) {
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			entries[filepath.ToSlash(rel)+"/"] = "directory"
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("continuation workspace contains nonregular entry %s", rel)
		}
		digest, err := fileDigest(path)
		if err != nil {
			return err
		}
		entries[filepath.ToSlash(rel)] = digest
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return nil, "", err
	}
	return entries, fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}

func recordJourneyContinuationMeta(dir string, provenance *journeyContinuation) error {
	path := filepath.Join(dir, "meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var meta struct {
		evalMetadata
		ProbeInputsSHA256 string               `json:"probe_inputs_sha256"`
		Continuation      *journeyContinuation `json:"diagnostic_continuation"`
	}
	err = json.Unmarshal(data, &meta)
	if err != nil {
		return err
	}
	meta.Continuation = provenance
	meta.Suite = "journey-diagnostic-continuation"
	meta.ProbeInputsSHA256 = meta.InputsSHA256
	inputs := struct {
		ProbeInputsSHA256 string               `json:"probe_inputs_sha256"`
		Continuation      *journeyContinuation `json:"diagnostic_continuation"`
	}{ProbeInputsSHA256: meta.ProbeInputsSHA256, Continuation: provenance}
	inputJSON, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	meta.InputsSHA256 = fmt.Sprintf("%x", sha256.Sum256(inputJSON))
	data, err = json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func TestJourneyContinuationCopiesActualInputAndPreservesSource(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	err := os.Mkdir(filepath.Join(source, "workspace"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(source, "workspace", "ledger.go"), []byte("partially repaired"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := conversation.New([]llm.Message{{Role: llm.RoleUser, Content: "repair"}, {Role: llm.RoleAssistant, Content: "incomplete"}})
	err = snapshot.Save(filepath.Join(source, "repair.state.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(source, "journey.json"), []byte(`{"repair":{"Outcome":"failed","Steps":24,"MaxSteps":24},"passed":false}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	before, sourceDigest, err := journeyWorkspaceDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	provenance, history, err := prepareJourneyContinuation(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(history, snapshot.History) || provenance.CombinedMaxSteps != 48 || provenance.AdditionalMaxSteps != 24 || provenance.PriorRepairSteps != 24 {
		t.Fatalf("lost history or budget: %+v, %v", provenance, history)
	}
	if !strings.Contains(provenance.Note, filepath.Join(destination, "workspace")) || !strings.HasPrefix(provenance.Note, journeyContinuationNote) {
		t.Fatalf("continuation note lost relocated workspace: %s", provenance.Note)
	}
	archiveHash, err := fileDigest(filepath.Join(destination, "repair.state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if archiveHash != provenance.ArchiveSHA256 {
		t.Fatal("archive hash does not identify copied input")
	}
	err = os.WriteFile(filepath.Join(destination, "workspace", "ledger.go"), []byte("changed only in continuation"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	after, afterDigest, err := journeyWorkspaceDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	if sourceDigest != afterDigest || !reflect.DeepEqual(before, after) {
		t.Fatal("continuation changed source evidence")
	}
	err = verifyJourneyContinuationSource(provenance)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(destination, "meta.json"), []byte(`{"suite":"journey","keep_workspaces":true,"tool_profile":"baseline","binary_sha256":"binary","inputs_sha256":"fresh-probe-inputs"}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = recordJourneyContinuationMeta(destination, provenance)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(destination, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), provenance.WorkspaceSHA256) || !strings.Contains(string(raw), "journey-diagnostic-continuation") || !strings.Contains(string(raw), `"binary_sha256": "binary"`) {
		t.Fatalf("continuation metadata lost provenance: %s", raw)
	}
	var recorded struct {
		InputsSHA256      string `json:"inputs_sha256"`
		ProbeInputsSHA256 string `json:"probe_inputs_sha256"`
	}
	err = json.Unmarshal(raw, &recorded)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.ProbeInputsSHA256 != "fresh-probe-inputs" || len(recorded.InputsSHA256) != 64 || recorded.InputsSHA256 == recorded.ProbeInputsSHA256 {
		t.Fatalf("continuation input identity still represents fresh seed: %+v", recorded)
	}
	_, _, err = prepareJourneyContinuation(source, source)
	if err == nil {
		t.Fatal("accepted source directory as output")
	}
	err = os.WriteFile(filepath.Join(source, "workspace", "ledger.go"), []byte("unexpected modification"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = verifyJourneyContinuationSource(provenance)
	if err == nil {
		t.Fatal("source workspace mutation was not detected")
	}
}
