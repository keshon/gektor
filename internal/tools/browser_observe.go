package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
)

// BrowserObserve packages current observations, never a task-completion verdict.
// It is installed with the browser capability, not in the startup tool set.
type BrowserObserve struct {
	Tools  *agent.Registry
	mu     sync.Mutex
	images map[string][][]string
}

// Browser-side extraction avoids transferring source HTML or an entire page to
// the model. Hidden article templates must not win over the rendered content.
const browserTextScript = `
const visible = el => el && el.getClientRects().length > 0 && getComputedStyle(el).visibility === 'visible';
const firstVisible = selector => Array.from(document.querySelectorAll(selector)).find(visible);
const root = firstVisible('article') || firstVisible('main') || document.body;
const bodyText = visible(document.body) ? document.body.innerText : '';
const chars = Array.from(visible(root) ? root.innerText : '');
const offset = Math.min(p.offset, chars.length);
const end = Math.min(offset + 3000, chars.length);
return {
  url: location.href.slice(0, 2000), title: document.title.slice(0, 500),
  metadata_truncated: location.href.length > 2000 || document.title.length > 500,
  scope: root?.tagName.toLowerCase() || 'body', text: chars.slice(offset, end).join(''),
  offset, next_offset: end < chars.length ? end : 0, total_characters: chars.length,
  ...(p.expect ? {expectation_met: bodyText.includes(p.expect), check: 'literal_visible_body_text'} : {})
};`

func (*BrowserObserve) Name() string         { return "browser_observe" }
func (*BrowserObserve) Mode() agent.ToolMode { return agent.Exclusive }
func (*BrowserObserve) Description() string {
	return "Read page text (default), DOM controls/refs, or a screenshot (image input required). expect_text asserts literal visible body text; mismatch fails. Verify persistence after reload; observations alone do not prove task completion."
}
func (*BrowserObserve) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["text","controls","visual"]},"offset":{"type":"integer","minimum":0,"description":"Text mode only: zero-based Unicode character offset. Continue with next_offset; absent means end."},"expect_text":{"type":"string","minLength":1,"maxLength":1000}},"additionalProperties":false}`)
}

func canObserveBrowser(defs []llm.ToolDef) bool {
	needed := map[string]bool{"mcp__browser__browser_evaluate": false, "mcp__browser__browser_snapshot": false, "mcp__browser__browser_take_screenshot": false}
	for _, def := range defs {
		if _, ok := needed[def.Name]; ok {
			needed[def.Name] = true
		}
	}
	for _, found := range needed {
		if !found {
			return false
		}
	}
	return true
}

func (t *BrowserObserve) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := rejectUnknownFields(args, "mode", "offset", "expect_text"); err != nil {
		return "", err
	}
	var in struct {
		Mode   string `json:"mode"`
		Offset int    `json:"offset"`
		Expect string `json:"expect_text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if in.Mode == "" {
		in.Mode = "text"
	}
	if in.Mode != "text" && in.Mode != "controls" && in.Mode != "visual" || in.Offset < 0 || in.Mode != "text" && in.Offset != 0 || len([]rune(in.Expect)) > 1000 {
		return "", fmt.Errorf("invalid browser observation mode, offset or expectation")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.Tools == nil {
		return "", fmt.Errorf("browser is not connected")
	}
	if in.Mode == "visual" {
		if enabled, _ := ctx.Value(visionContextKey{}).(bool); !enabled {
			return "", fmt.Errorf("visual observation unavailable: model has no vision capability; use text or controls, or select a vision-capable backend")
		}
	}
	var page struct {
		URL               string `json:"url"`
		Title             string `json:"title"`
		Scope             string `json:"scope"`
		Text              string `json:"text"`
		Offset            int    `json:"offset"`
		Next              int    `json:"next_offset,omitempty"`
		Total             int    `json:"total_characters"`
		Expected          *bool  `json:"expectation_met,omitempty"`
		Check             string `json:"check,omitempty"`
		Notice            string `json:"notice,omitempty"`
		MetadataTruncated bool   `json:"metadata_truncated,omitempty"`
	}
	if in.Mode == "text" || in.Expect != "" {
		params, _ := json.Marshal(struct {
			Offset int    `json:"offset"`
			Expect string `json:"expect"`
		}{in.Offset, in.Expect})
		function := `() => { const p = ` + string(params) + ";" + browserTextScript + "}"
		input, _ := json.Marshal(map[string]string{"function": function})
		out, err := t.Tools.Run(ctx, "mcp__browser__browser_evaluate", input)
		if err != nil {
			return out, err
		}
		if err := decodeBrowserResult(out, &page); err != nil {
			return "", err
		}
		if page.URL == "" || page.Total < 0 || page.Offset < 0 {
			return "", fmt.Errorf("browser returned incomplete page observation metadata")
		}
		if in.Offset > page.Total {
			return "", fmt.Errorf("offset exceeds page text length (%d characters)", page.Total)
		}
		if page.Total == 0 {
			page.Notice = "No rendered text found; this does not prove the page is blank. Use controls or visual for image/canvas content."
		}
		// Bound the encoded JSON too, including Unicode and escaped characters.
		for {
			data, _ := json.Marshal(page)
			if len(data) <= browserObservationMax {
				break
			}
			text := []rune(page.Text)
			if len(text) == 0 {
				return "", fmt.Errorf("page metadata exceeded observation budget")
			}
			page.Text = string(text[:len(text)/2])
			page.Next = page.Offset + len([]rune(page.Text))
		}
		if in.Expect != "" && (page.Expected == nil || !*page.Expected) {
			data, _ := json.Marshal(page)
			return string(data), fmt.Errorf("visible-text expectation was not met; action success alone is not verification")
		}
	}
	if in.Mode == "text" {
		data, _ := json.Marshal(page)
		return string(data), nil
	}
	name := "mcp__browser__browser_snapshot"
	if in.Mode == "visual" {
		name = "mcp__browser__browser_take_screenshot"
	}
	input := json.RawMessage(`{}`)
	if in.Mode == "visual" {
		input = json.RawMessage(`{"scale":"css","type":"png"}`)
	}
	out, err := t.Tools.Run(ctx, name, input)
	if err != nil {
		return out, err
	}
	if in.Mode == "visual" {
		at := t.Tools.AttacherOf(name)
		if at == nil {
			return out, fmt.Errorf("screenshot returned without an image attachment")
		}
		paths, err := at.Attach(input)
		if err != nil || len(paths) == 0 {
			return out, fmt.Errorf("visual observation unavailable: screenshot pixels were not delivered")
		}
		t.mu.Lock()
		if t.images == nil {
			t.images = make(map[string][][]string)
		}
		t.images[string(args)] = append(t.images[string(args)], paths)
		t.mu.Unlock()
	} else if strings.Contains(out, "[browser observation unavailable:") {
		return out, fmt.Errorf("current controls could not be observed; inspect state before another action")
	}
	if in.Expect != "" {
		out = "Visible body text expectation met (does not prove persistence).\n" + out
	}
	return truncateBrowserOutput(out, browserObservationMax), nil
}

func decodeBrowserResult(output string, result any) error {
	const marker = "### Result\n"
	_, body, ok := strings.Cut(strings.ReplaceAll(output, "\r\n", "\n"), marker)
	if !ok {
		return fmt.Errorf("browser returned no structured observation result")
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "```") {
		_, body, _ = strings.Cut(body, "\n")
	}
	if err := json.NewDecoder(strings.NewReader(body)).Decode(result); err != nil {
		return fmt.Errorf("invalid browser observation result: %w", err)
	}
	return nil
}

func (t *BrowserObserve) Attach(args json.RawMessage) ([]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	queue := t.images[string(args)]
	if len(queue) == 0 {
		return nil, nil
	}
	paths := queue[0]
	if len(queue) == 1 {
		delete(t.images, string(args))
	} else {
		t.images[string(args)] = queue[1:]
	}
	return paths, nil
}
