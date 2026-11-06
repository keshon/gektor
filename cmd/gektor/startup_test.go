package main

import (
	"testing"

	"github.com/keshon/gektor/internal/config"
)

func TestTUIStartupScreenKeepsSavedSetup(t *testing.T) {
	p := config.DefaultProfile()
	p.Provider.Model = "saved-model"
	prefs := config.Preferences{Version: 1, Profile: &p, Workspace: "saved-project", Mode: "act"}
	for _, screen := range []string{"agent", "workflows"} {
		o, err := parseLaunchOptions([]string{"-tui", "-tui-screen", screen})
		if err != nil {
			t.Fatal(err)
		}
		launch := initialTUILaunch(o, prefs, "application.json")
		resolved, err := resolveLaunch(launch)
		if err != nil || !launch.common || resolved.tuiScreen != screen || resolved.model != "saved-model" || resolved.workspace != "saved-project" || resolved.plan {
			t.Fatal("startup screen changed the saved setup", resolved, err)
		}
	}
}

func TestTUIStartupScreenRejectsConflictingLaunches(t *testing.T) {
	for _, args := range [][]string{
		{"-tui-screen", "workflows"},
		{"-tui", "-tui-screen", "unknown"},
		{"-tui", "-tui-screen", "workflows", "do a task"},
		{"-tui", "-tui-screen", "workflows", "-resume", "state.json"},
		{"-tui", "-tui-screen", "workflows", "-fork", "state.json"},
		{"-tui", "-tui-screen", "workflows", "-serve"},
	} {
		if _, err := parseLaunchOptions(args); err == nil {
			t.Fatal("accepted conflicting startup", args)
		}
	}
}
