package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
)

type InspectContext struct{}

func (InspectContext) Name() string         { return "inspect_context" }
func (InspectContext) Mode() agent.ToolMode { return agent.Concurrent }
func (InspectContext) Description() string {
	return "Inspect this call's model request, not backend internals. Default overview. Instructions: character offset; lists: item offset."
}
func (InspectContext) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"view":{"type":"string","enum":["overview","instructions","tools","skills","revisions"]},"section":{"type":"string"},"revision":{"type":"integer","minimum":1},"offset":{"type":"integer","minimum":0}},"additionalProperties":false}`)
}

func (InspectContext) Run(ctx context.Context, args json.RawMessage) (string, error) {
	v, ok := agent.InspectionFrom(ctx)
	if !ok {
		return "", fmt.Errorf("no agent request is available in this tool context")
	}
	var in struct {
		View, Section    string
		Revision, Offset int
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if in.View == "" {
		in.View = "overview"
	}
	if in.Offset < 0 || in.Revision < 0 {
		return "", fmt.Errorf("revision and offset must be nonnegative")
	}
	if in.View != "instructions" && (in.Section != "" || in.Revision != 0) {
		return "", fmt.Errorf("section and revision require view instructions")
	}
	out := map[string]any{"source": "model_request", "request": v.Request.Number}
	page := func(total, limit int) (int, error) {
		if in.Offset > total {
			return 0, fmt.Errorf("offset exceeds %d", total)
		}
		end := min(in.Offset+limit, total)
		out["total"], out["offset"] = total, in.Offset
		if end < total {
			out["next_offset"] = end
		}
		return end, nil
	}
	switch in.View {
	case "overview":
		r := v.Request
		end, err := page(max(len(r.Included), len(r.Rewritten)), 20)
		if err != nil {
			return "", err
		}
		out["included_range_count"], out["rewritten_count"] = len(r.Included), len(r.Rewritten)
		r.Included = r.Included[min(in.Offset, len(r.Included)):min(end, len(r.Included))]
		r.Rewritten = r.Rewritten[min(in.Offset, len(r.Rewritten)):min(end, len(r.Rewritten))]
		out["scope"] = "Agent input before provider formatting; archive presence does not mean inclusion."
		out["manifest"], out["tool_count"], out["retained_skill_load_count"] = r, len(v.Tools), len(v.Skills)
		p := v.Prompts[v.Request.PromptRevision-1]
		sections := []string{"system", "directive"}
		for _, s := range p.Sections {
			sections = append(sections, s.Name)
		}
		if v.Request.HasSummary {
			sections = append(sections, "summary")
		}
		out["instruction_sections"] = sections
	case "instructions":
		id := in.Revision
		if id == 0 {
			id = v.Request.PromptRevision
		}
		if id < 1 || id > len(v.Prompts) {
			return "", fmt.Errorf("unknown prompt revision %d", id)
		}
		p := v.Prompts[id-1]
		if in.Section == "" {
			in.Section = "system"
		}
		text := p.Text
		switch in.Section {
		case "system":
		case "directive", "summary":
			if in.Revision != 0 {
				return "", fmt.Errorf("%s is available only for the current request", in.Section)
			}
			if in.Section == "directive" {
				text = v.Directive
			} else {
				text = v.Summary
			}
		default:
			found := false
			for _, section := range p.Sections {
				if section.Name == in.Section {
					text, found = p.Text[section.Start:section.End], true
					break
				}
			}
			if !found {
				return "", fmt.Errorf("unknown instruction section %q", in.Section)
			}
		}
		runes := []rune(text)
		end, err := page(len(runes), 2000)
		if err != nil {
			return "", err
		}
		out["section"], out["text"] = in.Section, string(runes[in.Offset:end])
		if in.Section != "directive" && in.Section != "summary" {
			out["revision"], out["source"], out["sha256"] = p.ID, p.Source, p.SHA256
			out["current"] = p.ID == v.Request.PromptRevision
		}
	case "tools":
		end, err := page(len(v.Tools), 20)
		if err != nil {
			return "", err
		}
		out["tools"] = v.Tools[in.Offset:end]
		out["scope"] = "Advertised in this request. Role allowance does not override permissions."
	case "skills":
		end, err := page(len(v.Skills), 20)
		if err != nil {
			return "", err
		}
		out["skills"] = v.Skills[in.Offset:end]
		out["scope"] = "Successful load_skill records retained in this request, not the available skill catalog or proof that full skill text remains."
	case "revisions":
		end, err := page(len(v.Prompts), 10)
		if err != nil {
			return "", err
		}
		type revision struct {
			ID           int                          `json:"id"`
			FirstRequest int                          `json:"first_request,omitempty"`
			Source       string                       `json:"source"`
			SHA256       string                       `json:"sha256"`
			Sections     []conversation.PromptSection `json:"sections,omitempty"`
		}
		rows := []revision{}
		for _, p := range v.Prompts[in.Offset:end] {
			rows = append(rows, revision{p.ID, p.FirstRequest, p.Source, p.SHA256, p.Sections})
		}
		out["revisions"] = rows
		out["scope"] = "Saved system text versions. saved_archive means original delivery is unknown; no versions can be recovered from before tracking began."
	default:
		return "", fmt.Errorf("unknown context view %q", in.View)
	}
	data, err := json.Marshal(out)
	return string(data), err
}
