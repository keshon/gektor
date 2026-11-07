package tui

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/transcript"
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

func toolSummary(text string) (name, action, target string) {
	name, args, _ := strings.Cut(text, " ")
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(args), &fields)
	value := func(key string) string {
		var s string
		_ = json.Unmarshal(fields[key], &s)
		return strings.Join(strings.Fields(s), " ")
	}
	argument := func(key string) string {
		return strings.TrimSpace(string(fields[key]))
	}
	command := func() string {
		parts := []string{value("program")}
		var argv []string
		_ = json.Unmarshal(fields["args"], &argv)
		for _, arg := range argv {
			if arg == "" || strings.ContainsAny(arg, " \t\r\n\"\\") {
				arg = strconv.Quote(arg)
			}
			parts = append(parts, arg)
		}
		return strings.Join(parts, " ")
	}
	action, target = name, args
	switch name {
	case "system_info":
		action, target = "System info", value("group")
		if target == "skills" {
			action, target = "List skills", value("filter")
		} else if target == "tools" {
			action, target = "List tools", value("filter")
		} else {
			action = "Inspect system"
		}
	case "read_file":
		action, target = "Read file", value("path")
	case "image_view":
		action, target = "View image", value("path")
		if argument("crop") != "" {
			target += " [crop]"
		}
	case "image_download":
		action, target = "Download image", value("url")
	case "image_attachments":
		action, target = "List images", "session attachments"
	case "write_file":
		action, target = "Write file", value("path")
	case "patch_file":
		action, target = "Patch file", value("path")
	case "list_files":
		action, target = "List files", value("path")
	case "move_file":
		action, target = "Move file", value("from")+" → "+value("to")
	case "grep_files":
		action, target = "Search files", strconv.Quote(value("pattern"))+" in "+value("path")
		if glob := value("glob"); glob != "" {
			target += " [" + glob + "]"
		}
	case "run_shell":
		action, target = "Run command", value("command")
	case "run_program":
		action, target = "Run program", command()
	case "start_background":
		action, target = "Start command", value("command")
	case "start_program":
		action, target = "Start program", command()
	case "background_process":
		action, target = "Background process", value("id")
	case "check_background", "wait_background", "stop_background":
		action = map[string]string{"check_background": "Check process", "wait_background": "Wait for process", "stop_background": "Stop process"}[name]
		target = value("id")
	case "read_output":
		action, target = "Read output", value("id")
		if offset := argument("offset"); offset != "" {
			target += " from byte " + offset
		}
	case "git":
		action, target = "Git", value("subcommand")
		if argument("staged") == "true" {
			target += " --staged"
		}
		for _, key := range []string{"ref", "path"} {
			if v := value(key); v != "" {
				target += " " + v
			}
		}
	case "git_add":
		action, target = "Stage files", value("path")
	case "git_commit":
		action, target = "Commit", strconv.Quote(value("message"))
	case "search_web":
		action, target = "Search web", value("query")
	case "webfetch", "fetch_raw", "check_url", "cap__social__read_post":
		action = map[string]string{"webfetch": "Read page", "fetch_raw": "Fetch raw", "check_url": "Check URL", "cap__social__read_post": "Read post"}[name]
		target = value("url")
	case "enable_browser":
		action, target = "Enable browser", ""
	case "browser_observe":
		action, target = "Observe browser", value("mode")
		if target == "" {
			target = "text"
		}
	case "discover_capabilities":
		action, target = "Find capabilities", value("query")
	case "enable_capability":
		action, target = "Enable capability", value("id")
	case "load_skill":
		action, target = "Load skill", strings.TrimSpace(value("id")+" "+value("resource"))
	case "read_history":
		action, target = "Read history", "from "+argument("start")
		if argument("start") == "" {
			target = "from 1"
		}
		if count := argument("count"); count != "" {
			target += ", count " + count
		}
		if query := value("query"); query != "" {
			action, target = "Search history", strconv.Quote(query)
		}
	case "todo":
		var items []json.RawMessage
		_ = json.Unmarshal(fields["items"], &items)
		action, target = "Update tasks", strconv.Itoa(len(items))+" items"
	case "ask_user":
		action, target = "Ask user", value("question")
	case "delegate_task":
		action, target = "Delegate", strings.TrimSpace(value("role")+": "+value("task"))
	case "submit_evaluation":
		action, target = "Submit evaluation", value("summary")
	}
	if target == "" && action == name {
		target = args
	}
	return name, action, target
}

func toolState(b block) transcript.ActionState {
	if b.open {
		if b.returned {
			return transcript.ActionReturned
		}
		return transcript.ActionRunning
	}
	if b.toolStatus == "skipped_for_input" {
		return transcript.ActionSkipped
	}
	if b.failed {
		return transcript.ActionFailed
	}
	return transcript.ActionCompleted
}

func toolActionStyles(st styles) transcript.ActionStyles {
	return transcript.ActionStyles{Secondary: st.dim, Success: st.add, Warning: st.warn, Error: st.err}
}

func compactTool(b block, st styles, width int) string {
	name, action, target := toolSummary(b.text)
	failure := ""
	if b.failed && !b.open {
		for _, line := range strings.Split(b.result, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "error:") {
				failure = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "error:"))
				break
			}
		}
	}
	return (transcript.ActionRow{Action: action, Target: target, Name: name, Failure: failure, State: toolState(b)}).View(width, toolActionStyles(st))
}

// receiptText changes presentation only; the original evidence stays in the block.
func receiptText(raw string, st styles, width int) []string {
	fields := map[string]string{}
	current := ""
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if ok && (key == "outcome" || key == "files" || key == "checks" || key == "resume" || key == "summary" || key == "warnings" || key == "open_todos") {
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
	details := []string{}
	for _, field := range []struct{ key, label string }{{"summary", "Progress"}, {"warnings", "Warning"}, {"open_todos", "Remaining"}} {
		if value := fields[field.key]; value != "" {
			details = append(details, field.label+": "+value)
		}
	}
	if files := fields["files"]; files != "" && files != "none observed" {
		details = append(details, "Observed changes: "+files)
	}
	if checks != "" && checks != "no current command verdict" {
		details = append(details, "Checks: "+checks)
	}
	if resume := strings.TrimPrefix(fields["resume"], "gektor -tui -resume "); resume != "" {
		id := resume
		if filepath.Base(resume) == "state.json" {
			id = filepath.Base(filepath.Dir(resume))
		}
		details = append(details, "Session: "+id, "Path: "+resume)
		details = append(details, "Resume: gektor -tui -resume \""+resume+"\"")
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
