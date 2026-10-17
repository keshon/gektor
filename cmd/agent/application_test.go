package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/config"
)

// The relaunch has to carry the profile file, the workspace and plan mode, so
// a session restarted from Settings comes back up where it left off.
func TestApplicationArgsCarryProfileWorkspaceAndPlan(t *testing.T) {
	p, err := config.LoadPreferences(t.TempDir() + "/missing.json")
	if err != nil {
		t.Fatal(err)
	}
	p.ProfilePath = "profiles/local.json"
	p.Workspace = "D:/work"
	args := strings.Join(applicationArgs(p), " ")
	for _, want := range []string{"-tui", "-profile profiles/local.json", "-workspace D:/work", "-plan"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in %q", want, args)
		}
	}
	if strings.Contains(args, "-voice") || strings.Contains(args, "-conversation") {
		t.Fatalf("removed flag still emitted: %q", args)
	}
}
// A relaunch must come back up on the backend the session was created
// against. Rebuilding argv with an empty "-profile" parsed cleanly and
// then silently reverted to the flag defaults, and the profile handed
// over in memory was only read inside the profilePath != "" branch — so
// both halves dropped it.
func TestRelaunchKeepsTheProfileItRanWith(t *testing.T) {
	p := config.Preferences{Version: 1, Workspace: t.TempDir()}
	args := applicationArgs(p)
	if strings.Contains(strings.Join(args, " "), "-profile") {
		t.Fatalf("a generated setup has no profile file: %q", args)
	}

	want := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://10.0.0.5:8080", Model: "my-model"},
		Generation: config.Generation{MaxTokens: 4096, ContextLimit: 32768},
	}
	resolved, err := resolveLaunch(launchRequest{args: args, profile: &want})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.selectedProfile.Version != 1 {
		t.Fatalf("in-memory profile dropped: %+v", resolved.selectedProfile)
	}
	if resolved.backendURL != want.Provider.URL || resolved.model != want.Provider.Model || resolved.backendKind != want.Provider.Kind {
		t.Fatalf("relaunch lost the backend: kind=%q url=%q model=%q",
			resolved.backendKind, resolved.backendURL, resolved.model)
	}
	if resolved.maxTokens != want.Generation.MaxTokens || resolved.contextLimit != want.Generation.ContextLimit {
		t.Fatalf("relaunch lost generation: %d/%d", resolved.maxTokens, resolved.contextLimit)
	}
}

// The file still wins when nothing was handed over, and an explicit
// -profile still loads.
func TestRelaunchProfileFallbacksUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	want := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://127.0.0.1:9999", Model: "from-file"},
		Generation: config.Generation{MaxTokens: 512, ContextLimit: 4096},
	}
	if err := config.SaveCopy(want, path); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveLaunch(launchRequest{args: []string{"-tui", "-profile", path, "-workspace", dir}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.model != "from-file" || resolved.backendURL != want.Provider.URL {
		t.Fatalf("explicit -profile no longer loads: kind=%q url=%q model=%q",
			resolved.backendKind, resolved.backendURL, resolved.model)
	}
}

func TestAgentLaunchKeepsPermissionPlanBoundary(t *testing.T) {
	p := config.Preferences{ProfilePath: "profile.json", Workspace: "D:/work"}
	args := strings.Join(applicationArgs(p), " ")
	for _, want := range []string{"-profile profile.json", "-plan", "-workspace D:/work"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in %q", want, args)
		}
	}
	if strings.Contains(args, "-allow") {
		t.Fatal("launch granted permissions")
	}
}
func TestAgentServiceConfigurationSupportsTextOnlyProfile(t *testing.T) {
	p := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000}}
	cfg, err := app.ConfigureAgent(p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile.Version != 1 {
		t.Fatal("agent configuration lost its profile")
	}
	if cfg.Identity.System != config.DefaultSystemPrompt {
		t.Fatalf("default identity = %+v", cfg.Identity)
	}
}
func TestExecutionPolicyOptionsSurviveServiceHandoff(t *testing.T) {
	args := retainedAgentOptions([]string{"-tui", "-deny", "run_shell=*", "-pure", "-mcp", "local=command", "-model", "old"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-deny run_shell=*") || !strings.Contains(joined, "-pure") || !strings.Contains(joined, "-mcp local=command") || strings.Contains(joined, "-model") {
		t.Fatal(joined)
	}
}
