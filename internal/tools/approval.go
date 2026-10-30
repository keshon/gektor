package tools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/keshon/gektor/internal/workspace"
)

// ApprovalEvidence is operator-only. It must not be appended to model history.
// Its snapshot covers the source (and rename destination), including absence.
type ApprovalEvidence struct {
	Text     string
	versions map[string]string
}

func approvalVersion(ws *workspace.Workspace, path string) (string, []byte, error) {
	full, err := ws.Resolve(path)
	if err != nil {
		return "", nil, err
	}
	data, err := ws.ReadFile(full)
	if os.IsNotExist(err) {
		return "absent", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), data, nil
}

func (e ApprovalEvidence) Current(ws *workspace.Workspace) bool {
	for path, version := range e.versions {
		current, _, err := approvalVersion(ws, path)
		if err != nil || current != version {
			return false
		}
	}
	return true
}

// FullApproval builds the complete diff from the same bytes it fingerprints.
// Unsupported or unreadable proposals retain the ordinary bounded gate prompt.
func FullApproval(ws *workspace.Workspace, tool string, args json.RawMessage) ApprovalEvidence {
	e := ApprovalEvidence{versions: map[string]string{}}
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Old     string `json:"old_content"`
		New     string `json:"new_content"`
		From    string `json:"from"`
		To      string `json:"to"`
	}
	if json.Unmarshal(args, &in) != nil {
		return ApprovalEvidence{}
	}
	read := func(path string) (string, string, bool) {
		version, data, err := approvalVersion(ws, path)
		if err != nil || isUTF16(data) {
			return "", "", false
		}
		e.versions[path] = version
		text, _ := normalizeContent(data)
		return text, version, true
	}
	switch tool {
	case "patch_file", "write_file":
		before, version, ok := read(in.Path)
		if !ok {
			return ApprovalEvidence{}
		}
		after, _ := normalizeContent([]byte(in.Content))
		if tool == "patch_file" {
			old, _ := normalizeContent([]byte(in.Old))
			newText, _ := normalizeContent([]byte(in.New))
			if version == "absent" || guardEdit(tool, old, newText) != nil || strings.Count(before, old) != 1 {
				return ApprovalEvidence{}
			}
			after = strings.Replace(before, old, newText, 1)
		}
		e.Text = unifiedDiff(in.Path, before, after)
		if before == after {
			if version == "absent" {
				e.Text = fmt.Sprintf("(new empty file: %s)", in.Path)
			} else {
				e.Text = "(identical to the file on disk — this write changes nothing)"
			}
		}
	case "move_file":
		_, version, ok := read(in.From)
		if !ok || version == "absent" {
			return ApprovalEvidence{}
		}
		if _, _, ok := read(in.To); !ok {
			return ApprovalEvidence{}
		}
		e.Text = previewMove(args)
	default:
		return ApprovalEvidence{}
	}
	return e
}
