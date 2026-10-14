package main

import (
	"github.com/keshon/tars/internal/config"
	"strings"
	"testing"
)

func TestApplicationDefaultsDoNotEnableSpeechOrTools(t *testing.T) {
	p, err := config.LoadPreferences(t.TempDir() + "/missing.json")
	if err != nil {
		t.Fatal(err)
	}
	p.ProfilePath = "profiles/chat.json"
	args := strings.Join(applicationArgs(p), " ")
	if strings.Contains(args, "-voice") || !strings.Contains(args, "-conversation chat") {
		t.Fatal(args)
	}
}
func TestAgentLaunchKeepsPermissionPlanBoundary(t *testing.T) {
	p := config.Preferences{Mode: "agent", Voice: "live", ProfilePath: "profile.json", Workspace: "D:/work"}
	args := strings.Join(applicationArgs(p), " ")
	for _, want := range []string{"-conversation agent", "-voice", "-plan", "-workspace D:/work"} {
		if !strings.Contains(args, want) {
			t.Fatal(args)
		}
	}
	if strings.Contains(args, "-allow") {
		t.Fatal("launch granted permissions")
	}
}
func TestAgentServiceConfigurationSupportsTextOnlyProfile(t *testing.T) {
	p := config.Profile{Version: 1}
	cfg := configureAgentServices(p, t.TempDir())
	if cfg.Configure == nil || cfg.NewVoice != nil {
		t.Fatal("text-only agent lacks settings or advertises voice")
	}
}
func TestExecutionPolicyOptionsSurviveServiceHandoff(t *testing.T) {
	args := retainedAgentOptions([]string{"-tui", "-deny", "run_shell=*", "-pure", "-mcp", "local=command", "-model", "old"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-deny run_shell=*") || !strings.Contains(joined, "-pure") || !strings.Contains(joined, "-mcp local=command") || strings.Contains(joined, "-model") {
		t.Fatal(joined)
	}
}
