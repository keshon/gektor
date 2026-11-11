// Package release owns the distribution manifest, staging and release archives.
// It uses only the standard library and the installed Go and Git commands.
package release

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

func windowsFiles() []string {
	return []string{
		"start-gektor-agent.cmd", "start-gektor-workflows.cmd",
		"run-gektor-agent.cmd", "run-gektor-workflow.cmd", "serve-gektor-workflows.cmd",
		"scripts/run-task.cmd", "scripts/run-plan.cmd", "scripts/run-resume.cmd", "scripts/ensure-build.cmd",
		"demos/agent-task.cmd", "demos/workflow-draft.cmd", "demos/workflow-review.cmd", "demos/demo.ps1",
	}
}

func runtimeFiles(tracked []string, target string) []string {
	selected := map[string]bool{}
	for _, name := range tracked {
		switch name {
		case "README.md", "LICENSE", "SECURITY.md", "ARCHITECTURE.md", ".env.example",
			"eval/README.md", "demos/README.md", "scripts/README.md", "internal/workspace/builtin/workflows/SKILL.md":
			selected[name] = true
		default:
			runtimeAsset := strings.HasPrefix(name, "skills/") ||
				strings.HasPrefix(name, "profiles/") ||
				strings.HasPrefix(name, "examples/workflows/") ||
				strings.HasPrefix(name, "docs/images/")
			publicDocument := strings.HasPrefix(name, "docs/") &&
				strings.HasSuffix(name, ".md") &&
				!strings.HasPrefix(name, "docs/internal/") &&
				!strings.HasPrefix(name, "docs/design/")
			if runtimeAsset || publicDocument {
				selected[name] = true
			}
		}
	}

	if strings.HasPrefix(target, "windows-") {
		for _, name := range windowsFiles() {
			selected[name] = true
		}
	}

	files := make([]string, 0, len(selected))
	for name := range selected {
		files = append(files, name)
	}
	slices.Sort(files)

	return files
}

const markdownLinkPattern = `\]\(([^\s)]+)\)`
const absoluteLinkPattern = `^[a-zA-Z][a-zA-Z0-9+.-]*:|^//`

func releaseBytes(name string, data []byte, included map[string]bool, revision string) []byte {
	markdown := strings.HasSuffix(name, ".md")
	frozenSkill := strings.HasPrefix(name, "skills/") || strings.HasSuffix(name, "/SKILL.md")
	if !markdown || frozenSkill {
		return data
	}

	markdownLink := regexp.MustCompile(markdownLinkPattern)
	absoluteLink := regexp.MustCompile(absoluteLinkPattern)
	return markdownLink.ReplaceAllFunc(data, func(match []byte) []byte {
		href := string(match[2 : len(match)-1])
		if absoluteLink.MatchString(href) {
			return match
		}

		location, fragment, hasFragment := strings.Cut(href, "#")
		target := name
		if location != "" {
			directory := path.Dir(name)
			joined := path.Join(directory, location)
			target = path.Clean(joined)
		}
		if included[target] {
			return match
		}

		link := "](https://github.com/keshon/gektor/blob/" + revision + "/" + target
		if hasFragment {
			link += "#" + fragment
		}
		return []byte(link + ")")
	})
}
