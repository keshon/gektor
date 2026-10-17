package tui

// appliedSections are the staged sections Resolve actually writes.
// Profiles and Application are not among them: the first is saved with
// Ctrl+D, the second applied from its own page. Their buttons still read
// as live because the draft is dirty, so applying has to say plainly that
// nothing was applied rather than claim a success that did not happen.
func appliedSections(ws *settingsWorkspace) []string {
	var out []string
	for _, section := range ws.Draft.ChangedSections() {
		if section != "profiles" && section != "application" {
			out = append(out, section)
		}
	}
	return out
}