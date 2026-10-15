package session

import "strings"

const ReferenceStart = "\n\n<attached_workspace_files>\n"
const ReferenceEnd = "\n</attached_workspace_files>"

// DisplayInput removes the appended file snapshots from an archived user prompt.
func DisplayInput(text string) string {
	if strings.HasSuffix(text, ReferenceEnd) {
		if original, _, ok := strings.Cut(text, ReferenceStart); ok {
			return original
		}
	}
	return text
}
