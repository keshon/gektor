package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
)

func TestResumeWorkspaceSelection(t *testing.T) {
	saved, override := t.TempDir(), t.TempDir()
	path := filepath.Join(t.TempDir(), "state.json")
	for _, tc := range []struct{ name, workspace, explicit, want, failure string }{
		{name: "saved beats default", workspace: saved, want: saved},
		{name: "explicit override", workspace: saved, explicit: override, want: override},
		{name: "legacy", failure: "unassigned"},
		{name: "missing", workspace: filepath.Join(saved, "missing"), failure: "unavailable"},
		{name: "legacy override", explicit: override, want: override},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := conversation.New(nil)
			snapshot.Workspace = tc.workspace
			if err := snapshot.Save(path); err != nil {
				t.Fatal(err)
			}
			options := launchOptions{resume: path, workspace: override, explicitFlags: map[string]bool{}}
			if tc.explicit != "" {
				options.workspace = tc.explicit
				options.explicitFlags["workspace"] = true
			}
			got, err := resumeWorkspace(resolvedLaunch{launchOptions: options})
			if tc.failure != "" {
				if err == nil || !strings.Contains(err.Error(), tc.failure) || !strings.Contains(err.Error(), "-workspace") {
					t.Fatalf("got %q, %v", got, err)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
			unchanged, err := conversation.Load(path)
			if err != nil || unchanged.Workspace != tc.workspace {
				t.Fatalf("resolution stamped archive: %+v, %v", unchanged, err)
			}
		})
	}
}

func TestPersistExplicitSettingsWorkspaceSwitch(t *testing.T) {
	old, next := t.TempDir(), t.TempDir()
	if err := persistSwitchedWorkspace(launchRequest{directory: t.TempDir(), reassignWorkspace: true}, next); err != nil {
		t.Fatalf("fresh conversation cannot switch workspace: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	snapshot := conversation.New(nil)
	snapshot.Workspace = old
	if err := snapshot.Save(path); err != nil {
		t.Fatal(err)
	}
	launch := launchRequest{directory: dir}
	if err := persistSwitchedWorkspace(launch, next); err != nil {
		t.Fatal(err)
	}
	loaded, _ := conversation.Load(path)
	if loaded.Workspace != old {
		t.Fatal("non-settings relaunch stamped archive")
	}
	launch.reassignWorkspace = true
	if err := persistSwitchedWorkspace(launch, filepath.Join(next, "missing")); err == nil {
		t.Fatal("accepted missing workspace")
	}
	loaded, _ = conversation.Load(path)
	if loaded.Workspace != old {
		t.Fatal("failed switch stamped archive")
	}
	if err := persistSwitchedWorkspace(launch, next); err != nil {
		t.Fatal(err)
	}
	loaded, _ = conversation.Load(path)
	if loaded.Workspace != next {
		t.Fatal("successful switch not persisted")
	}
}
