package agent

import (
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/conversation"
)

// renameSavedSession persists a title and returns its display value. An empty
// title removes the override and falls back to history, then the directory ID.
func renameSavedSession(directory, id, title string) (string, error) {
	name := conversation.TitleLine(title)
	display := name
	if display == "" {
		history, err := conversation.ReadHistory(filepath.Join(directory, "state.json"))
		if err == nil {
			display = conversation.TitleFor(history)
		}

		// An unreadable history still permits clearing its stored title.
		if display == "" {
			display = id
		}
	}

	err := conversation.WriteSessionTitle(directory, name)
	if err != nil {
		return "", err
	}

	return display, nil
}

// reassignSessionWorkspace checks the workspace captured at confirmation before
// updating the snapshot. False with no error means that assignment became stale.
func reassignSessionWorkspace(assignment sessionAssignment) (bool, error) {
	path := filepath.Join(assignment.directory, "state.json")
	snapshot, err := conversation.Load(path)
	if err != nil {
		return false, err
	}

	if snapshot.Workspace != assignment.previous {
		return false, nil
	}

	snapshot.Workspace = assignment.workspace
	_, err = snapshot.ResolveWorkspace()
	if err != nil {
		return false, err
	}

	err = snapshot.Save(path)

	return err == nil, err
}

// sessionDeleteTarget resolves both paths before checking direct membership in
// the library. The caller may remove only the returned absolute directory.
func sessionDeleteTarget(root, directory string) (string, bool) {
	target, err := filepath.Abs(directory)
	if err != nil {
		return "", false
	}

	library, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}

	if !strings.EqualFold(filepath.Dir(target), library) {
		return "", false
	}

	return target, true
}
