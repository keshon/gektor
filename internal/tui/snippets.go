package tui

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func compactToolText(text string) string {
	name, args, ok := strings.Cut(text, " ")
	var compact bytes.Buffer
	if ok && json.Compact(&compact, []byte(args)) == nil {
		return name + " " + compact.String()
	}
	return strings.Join(strings.Fields(text), " ")
}

// snippetRow keeps the status visible while the action yields to window width.
func snippetRow(left, right string, width int) string {
	if lipgloss.Width(right)+3 >= width {
		return cellLine(left, width)
	}
	left = ansi.Truncate(left, width-lipgloss.Width(right)-2, "…")
	return left + strings.Repeat(" ", width-lipgloss.Width(left)-lipgloss.Width(right)) + right
}

func compactTool(b block, st styles, width int) string {
	name, args, _ := strings.Cut(b.text, " ")
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(args), &fields)
	value := func(key string) string {
		var s string
		_ = json.Unmarshal(fields[key], &s)
		return strings.Join(strings.Fields(s), " ")
	}
	action, target := name, args
	switch name {
	case "system_info":
		action, target = "System info", value("group")
		if target == "skills" {
			action = "List skills"
		}
	case "read_file":
		action, target = "Read file", value("path")
	case "write_file":
		action, target = "Write file", value("path")
	case "patch_file":
		action, target = "Patch file", value("path")
	case "list_files":
		action, target = "List files", value("path")
	case "run_shell":
		action, target = "Run command", value("command")
	case "search_web":
		action, target = "Search web", value("query")
	case "webfetch", "fetch_raw", "check_url":
		action, target = "Fetch page", value("url")
	}
	if target == "" {
		target = args
	}
	glyph, right := st.add.Render("✓"), st.dim.Render(name)
	if b.open {
		glyph, right = st.dim.Render("…"), st.dim.Render("Running")
	} else if b.failed {
		failure := strings.TrimSpace(strings.TrimPrefix(b.result, "error:"))
		failure, _, _ = strings.Cut(failure, "\n")
		if failure == "" {
			failure = "Failed"
		}
		glyph, right = st.err.Render("×"), st.err.Render(ansi.Truncate(failure, min(32, max(width/3, 6)), "…"))
	}
	left := glyph + " " + action
	if target != "" {
		left += " " + st.dim.Render(target)
	}
	return snippetRow(left, right, max(width, 1))
}

// receiptText changes presentation only; the original evidence stays in the block.
func receiptText(raw string, st styles, width int) []string {
	fields := map[string]string{}
	current := ""
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if ok && (key == "outcome" || key == "files" || key == "checks" || key == "resume") {
			fields[key] = value
			current = key
		} else if current != "" {
			fields[current] += "\n" + line
		}
	}
	outcome := fields["outcome"]
	title := outcome
	if title != "" {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	style := lipgloss.NewStyle()
	if outcome == "failed" {
		style = st.err
	} else if outcome == "cancelled" || outcome == "incomplete" {
		style = st.warn
	}
	checks := fields["checks"]
	meta := "Verification recorded"
	if checks == "" || checks == "no current command verdict" {
		meta = "No verification result"
	}
	rows := []string{snippetRow(style.Render(title), st.dim.Render(meta), width), ""}
	files := fields["files"]
	if files == "none observed" || files == "" {
		files = "no changes observed"
	}
	details := []string{"Files: " + files, "Checks: " + checks}
	if resume := strings.TrimPrefix(fields["resume"], "gektor -tui -resume "); resume != "" {
		id := resume
		if filepath.Base(resume) == "state.json" {
			id = filepath.Base(filepath.Dir(resume))
		}
		details = append(details, "Session: "+id+" · available in Session → Open")
	}
	for _, detail := range details {
		for _, line := range strings.Split(detail, "\n") {
			for _, fragment := range reflow(line, max(width, 1)) {
				rows = append(rows, st.dim.Render(fragment))
			}
		}
	}
	return rows
}
