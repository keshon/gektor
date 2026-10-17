package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// toolResource extracts the policy-matched resource from a call's args:
// command for shell/background, path for file tools, url for check_url.
// toolResource picks the string a permission rule matches against. It
// reads the argument shape rather than the tool name: every tool this
// project ships carries a path, a command or a url, and a new tool is
// covered by using one of those instead of by being registered here.
func toolResource(args json.RawMessage) string {
	var fields struct {
		Command string `json:"command"`
		Path    string `json:"path"`
		From    string `json:"from"`
		To      string `json:"to"`
		URL     string `json:"url"`
	}
	if json.Unmarshal(args, &fields) != nil {
		return ""
	}
	switch {
	case fields.Path != "":
		return fields.Path
	case fields.From != "" || fields.To != "":
		return fields.From + "->" + fields.To
	case fields.Command != "":
		return fields.Command
	case fields.URL != "":
		return fields.URL
	}
	return string(args)
}

// mutatedPathsFromCall pulls workspace paths out of a mutating tool
// call's arguments: path, or from/to for a rename. A call with none of
// them contributes nothing rather than guessing.
func mutatedPathsFromCall(args json.RawMessage) []string {
	var fields struct {
		Path string `json:"path"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	if json.Unmarshal(args, &fields) != nil {
		return nil
	}
	var out []string
	for _, p := range []string{fields.Path, fields.From, fields.To} {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writePathFromArgs(args json.RawMessage) string {
	var fields struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &fields) != nil || fields.Path == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ReplaceAll(fields.Path, "\\", "/"), "./")
}

// parseDelegateMutations reads the structured DELEGATE header from a
// delegate_task result so the parent run can count subagent writes.
func parseDelegateMutations(content string) (int, []string) {
	if !strings.HasPrefix(content, "DELEGATE\n") {
		return 0, nil
	}
	mutations := 0
	var paths []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "mutations: ") {
			var n int
			if _, err := fmt.Sscanf(line, "mutations: %d", &n); err == nil {
				mutations = n
			}
			continue
		}
		if p, ok := strings.CutPrefix(line, "paths: "); ok {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
			continue
		}
		if line == "----" {
			break
		}
	}
	return mutations, paths
}
