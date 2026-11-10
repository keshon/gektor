package tools

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	workflow "github.com/keshon/gektor/internal/automation/agenttools"
	"github.com/keshon/gektor/internal/automation/records"
	"github.com/keshon/gektor/internal/capabilities/documents"
	doccontract "github.com/keshon/gektor/internal/capabilities/documents/contract"
	"github.com/keshon/gektor/internal/capabilities/files"
	filecontract "github.com/keshon/gektor/internal/capabilities/files/contract"
	imagetools "github.com/keshon/gektor/internal/capabilities/images/tools"
	"github.com/keshon/gektor/internal/capabilities/media"
	mediacontract "github.com/keshon/gektor/internal/capabilities/media/contract"
	socialcontract "github.com/keshon/gektor/internal/capabilities/social/contract"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

// PathResources gives permissions the same relative paths used by filesystem
// access. In particular, an absolute spelling cannot bypass a relative rule,
// and a move must be authorized for its destination as well as its source.
func PathResources(ws *workspace.Workspace, tool string, args json.RawMessage) ([]string, error) {
	if ws == nil {
		return nil, nil
	}
	var in struct{ Path, From, To string }
	var paths []string
	switch tool {
	case socialcontract.AgentName:
		return (socialcontract.Operation{}).Resources(args)
	case "media_status", "video_info", "video_transcript", "video_download", "media_inspect", "media_convert", "media_clip", "media_frame", "media_merge":
		return (mediacontract.Operation{Service: &media.Service{WS: ws}, Kind: tool}).Resources(args)

	case "docx_read", "xlsx_read", "pdf_read":
		return (doccontract.Operation{Service: documents.Service{WS: ws}, Kind: tool}).Resources(args)
	case "download_file", "archive_list", "archive_create", "archive_extract":
		return (filecontract.Operation{Service: files.Service{WS: ws}, Kind: tool}).Resources(args)
	case "workflow_schema":
		var in struct {
			Kind string `json:"record_kind"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		return []string{in.Kind}, nil
	case "record_read", "record_write":
		return records.Resources(args)
	case "workflow_inspect":
		var in struct {
			ID string `json:"run_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		return []string{in.ID}, nil
	case "read_file", "write_file", "patch_file", "list_files", "grep_files", "git", "git_add":
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		paths = []string{in.Path}
	case "move_file":
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		paths = []string{in.From, in.To}
	case "image_view":
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		if llm.IsAttachment(in.Path) {
			return []string{in.Path}, nil
		}
		paths = []string{in.Path}
	case "image_inspect", "image_preview", "image_variants", "image_compare":
		var err error
		paths, err = imagetools.NewTool(ws, strings.TrimPrefix(tool, "image_")).Resources(args)
		if err != nil {
			return nil, err
		}
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
		return nil, nil
	}
	for i, path := range paths {
		if llm.IsAttachment(path) && (tool == "image_inspect" || tool == "image_preview" || tool == "image_compare" || tool == "image_variants") {
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

// Base returns the standard tool set shared by the main agent and
// subagents. Pass extra tools (ask_user, delegate_task, read_history) for the main
// agent only. File-mutating tools run post-write deterministic checks
// from rules, reporting to sink; a nil sink or no rules builds unwrapped.
// vision enables reading pictures through read_file; it must come from a
// backend capability probe, not a guess.
func Base(ws *workspace.Workspace, procs *BackgroundProcesses, report FindingSink, rules []checks.Rule, vision bool, extra ...agent.Tool) *agent.Registry {
	return agent.NewRegistry(nativeTools(packageDependencies{WS: ws, Procs: procs, Report: report, Rules: rules, Vision: vision, Extra: extra}, false)...)
}

// ReadOnly returns a tool set that can inspect the workspace and running
// processes but cannot change anything — no writes, no shell, no process
// control. For workers whose job is to look and report (codebase mapping,
// final review): removing the mutating tools entirely beats prompting a
// weak model not to use them.
func ReadOnly(ws *workspace.Workspace, procs *BackgroundProcesses, vision bool) *agent.Registry {
	return agent.NewRegistry(nativeTools(packageDependencies{WS: ws, Procs: procs, Vision: vision}, true)...)
}
