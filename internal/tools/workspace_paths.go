package tools

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"github.com/keshon/gektor/internal/capabilities/documents"
	doccontract "github.com/keshon/gektor/internal/capabilities/documents/contract"
	"github.com/keshon/gektor/internal/capabilities/files"
	filecontract "github.com/keshon/gektor/internal/capabilities/files/contract"
	imagecontract "github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/capabilities/media"
	mediacontract "github.com/keshon/gektor/internal/capabilities/media/contract"
	socialcontract "github.com/keshon/gektor/internal/capabilities/social/contract"
	"github.com/keshon/gektor/internal/llm"
	workflow "github.com/keshon/gektor/internal/workflow/agenttools"
	"github.com/keshon/gektor/internal/workflow/records"
	"github.com/keshon/gektor/internal/workspace"
)

// WorkspacePaths interprets tool path arguments for workspace access and scoped guidance.
type WorkspacePaths struct{ WS *workspace.Workspace }

// Resources gives permissions the same relative paths used by filesystem
// access. In particular, an absolute spelling cannot bypass a relative rule,
// and a move must be authorized for its destination as well as its source.
func (p WorkspacePaths) Resources(tool string, args json.RawMessage) ([]string, error) {
	ws := p.WS
	if ws == nil {
		return nil, nil
	}

	// Capability contracts own both membership and canonical resource extraction.
	switch {
	case tool == socialcontract.AgentName:
		operation := socialcontract.Operation{}
		return operation.Resources(args)
	case slices.Contains(mediacontract.Names(), tool):
		operation := mediacontract.Operation{Service: &media.Service{WS: ws}, Kind: tool}
		return operation.Resources(args)
	case slices.Contains(doccontract.Names(), tool):
		operation := doccontract.Operation{Service: documents.Service{WS: ws}, Kind: tool}
		return operation.Resources(args)
	case slices.Contains(filecontract.Names(), tool):
		operation := filecontract.Operation{Service: files.Service{WS: ws}, Kind: tool}
		return operation.Resources(args)
	}

	imageKind, hasImagePrefix := strings.CutPrefix(tool, "image_")
	imageOperation := hasImagePrefix && slices.Contains(imagecontract.Names(), imageKind)
	var in struct{ Path, From, To string }
	var paths []string
	switch tool {
	case "workflow_schema":
		var in struct {
			Kind string `json:"record_kind"`
		}
		err := json.Unmarshal(args, &in)
		if err != nil {
			return nil, err
		}
		return []string{in.Kind}, nil
	case "record_read", "record_write":
		return records.Resources(args)
	case "workflow_inspect":
		var in struct {
			ID string `json:"run_id"`
		}
		err := json.Unmarshal(args, &in)
		if err != nil {
			return nil, err
		}
		return []string{in.ID}, nil
	case "read_file", "write_file", "patch_file", "list_files", "grep_files", "git", "git_add":
		err := json.Unmarshal(args, &in)
		if err != nil {
			return nil, err
		}
		paths = []string{in.Path}
	case "move_file":
		err := json.Unmarshal(args, &in)
		if err != nil {
			return nil, err
		}
		paths = []string{in.From, in.To}
	case "image_view":
		err := json.Unmarshal(args, &in)
		if err != nil {
			return nil, err
		}
		if llm.IsAttachment(in.Path) {
			return []string{in.Path}, nil
		}
		paths = []string{in.Path}
	case "workflow_schedule":
		var err error
		paths, err = workflow.ScheduleResources(args)
		if err != nil {
			return nil, err
		}
	case "workflow_run", "workflow_validate":
		var err error
		paths, err = workflow.Resources(args)
		if err != nil {
			return nil, err
		}
	default:
		if !imageOperation {
			return nil, nil
		}
		operation := NewImageOperation(ws, imageKind)
		var err error
		paths, err = operation.Resources(args)
		if err != nil {
			return nil, err
		}
	}

	for i, path := range paths {
		if imageOperation && llm.IsAttachment(path) {
			continue
		}
		rel, err := ws.Relative(path)
		if err != nil {
			return nil, err
		}
		paths[i] = filepath.ToSlash(rel)
	}
	return paths, nil
}

// Instructions loads distinct scoped guides for path, source, and destination.
// Attachment references have no workspace scope. Invalid arguments are left to
// tool validation and contribute no guidance here.
func (p WorkspacePaths) Instructions(args json.RawMessage) (string, error) {
	var in struct{ Path, From, To string }
	if json.Unmarshal(args, &in) != nil || p.WS == nil {
		return "", nil
	}
	var parts []string
	for _, path := range []string{in.Path, in.From, in.To} {
		if path == "" || llm.IsAttachment(path) {
			continue
		}
		text, err := p.WS.ScopedInstructions(path)
		if err != nil {
			return "", err
		}
		if text != "" && !slices.Contains(parts, text) {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n"), nil
}
