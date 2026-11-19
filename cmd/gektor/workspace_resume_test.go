package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tui"
)

func TestTUIStartupRecoversDeletedRememberedWorkspace(t *testing.T) {
	home, launchDirectory := t.TempDir(), t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Chdir(launchDirectory)
	removed := filepath.Join(t.TempDir(), "removed workspace")
	err := os.Mkdir(removed, 0700)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Remove(removed)
	if err != nil {
		t.Fatal(err)
	}
	profile := config.DefaultProfile()
	prefs := config.Preferences{
		Version:   1,
		Workspace: removed,
		Profile:   &profile,
		Mode:      "act",
		Theme:     "light",
		Harness:   config.Harness{MaxSteps: 42},
	}
	path := filepath.Join(home, ".gektor", "application.json")
	err = config.SavePreferences(path, prefs)
	if err != nil {
		t.Fatal(err)
	}

	// Exercise startup and workspace construction without a terminal or model server.
	err = runApplication([]string{"-tui", "-context-report"})
	if err != nil {
		t.Fatalf("deleted remembered workspace still prevents startup: %v", err)
	}
	loaded, err := config.LoadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	prefs.Workspace = ""
	if !reflect.DeepEqual(loaded, prefs) {
		t.Fatalf("recovery changed other settings: %+v", loaded)
	}
	_, err = os.Stat(removed)
	if !os.IsNotExist(err) {
		t.Fatal("recovery recreated the removed workspace", err)
	}
}

func TestWorkspaceRecoveryStartsFreshWithoutChangingTheArchive(t *testing.T) {
	for _, kind := range []string{"missing", "replaced by file"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "previous workspace")
			if kind == "replaced by file" {
				err := os.WriteFile(root, []byte("file"), 0600)
				if err != nil {
					t.Fatal(err)
				}
			}
			archive := filepath.Join(t.TempDir(), "state.json")
			snapshot := conversation.New([]llm.Message{{Role: llm.RoleUser, Content: "Previous task"}})
			snapshot.Workspace = root
			err := snapshot.Save(archive)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			options, err := parseLaunchOptions([]string{"-tui"})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "application.json")
			request := initialTUILaunch(options, config.Preferences{Version: 1, Workspace: root}, path)
			request.directory, request.draft = filepath.Dir(archive), "Previous draft"
			request.history = snapshot.History
			request.images = []string{"previous.png"}
			request.drafts = tui.Drafts{"previous": {}}
			request.reassignWorkspace = true

			fresh, err := recoverTUIWorkspace(request)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.options.workspace != "." || fresh.preferences.Workspace != "" {
				t.Fatal("recovery retained the missing workspace")
			}
			if fresh.directory != "" || len(fresh.history) != 0 || fresh.reassignWorkspace {
				t.Fatalf("recovery retained the previous workspace's session: %+v", fresh)
			}
			if fresh.draft != "" || len(fresh.images) != 0 || len(fresh.drafts) != 0 {
				t.Fatal("recovery moved an old workspace's draft into the new session")
			}
			after, err := os.ReadFile(archive)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("recovery changed the saved conversation", err)
			}
		})
	}
}

func TestWorkspaceRecoveryPreservesExplicitRequestsAndExistingFolders(t *testing.T) {
	for _, args := range [][]string{
		{"-tui", "-workspace", "explicit directory"},
		{"-tui", "-resume", "state.json"},
		{"-tui", "-fork", "state.json"},
		{"-tui"},
	} {
		options, err := parseLaunchOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(t.TempDir(), "missing")
		if len(args) == 1 {
			root = t.TempDir()
		}
		path := filepath.Join(t.TempDir(), "application.json")
		prefs := config.Preferences{Version: 1, Workspace: root}
		err = config.SavePreferences(path, prefs)
		if err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		request := initialTUILaunch(options, prefs, path)
		got, err := recoverTUIWorkspace(request)
		if err != nil || !reflect.DeepEqual(got, request) {
			t.Fatalf("recovery changed launch %v: %+v, %v", args, got, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("recovery rewrote unchanged preferences", args, err)
		}
	}
}

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
