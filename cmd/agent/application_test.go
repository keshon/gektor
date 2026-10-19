package main

import (
	"path/filepath"
	"testing"

	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/config"
)

func TestTUILaunchUsesSavedSettingsAndExplicitOverrides(t *testing.T) {
	profile := config.DefaultProfile()
	profile.Provider.Model = "saved-model"
	prefs := config.Preferences{Version: 1, Profile: &profile, Workspace: "saved-workspace"}
	for _, args := range [][]string{{"-tui"}, {"-tui", "-workspace", "other-workspace"}} {
		options, err := parseLaunchOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		launch := initialTUILaunch(options, prefs, "application.json", len(args) == 1)
		got, err := resolveLaunch(launch)
		if err != nil {
			t.Fatal(err)
		}
		wantWorkspace := "saved-workspace"
		if len(args) > 1 {
			wantWorkspace = "other-workspace"
		}
		if got.model != "saved-model" || got.backendKind != "llama" || got.workspace != wantWorkspace || got.allow != "" || (len(args) == 1 && !got.plan) {
			t.Fatalf("wrong TUI launch: %+v", got)
		}
	}
}

// A relaunch must come back up on the backend the session was created
// against. Rebuilding argv with an empty "-profile" parsed cleanly and
// then silently reverted to the flag defaults, and the profile handed
// over in memory was only read inside the profilePath != "" branch — so
// both halves dropped it.
func TestRelaunchKeepsTheProfileItRanWith(t *testing.T) {
	p := config.Preferences{Version: 1, Workspace: t.TempDir()}
	options, err := parseLaunchOptions([]string{"-tui"})
	if err != nil {
		t.Fatal(err)
	}
	next := relaunchOptions(options, p, false)

	want := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://10.0.0.5:8080", Model: "my-model"},
		Generation: config.Generation{MaxTokens: 4096, ContextLimit: 32768},
	}
	resolved, err := resolveLaunch(launchRequest{options: &next, profile: &want})
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
	before, err := parseLaunchOptions([]string{"-tui", "-deny", "run_shell=*", "-allow", "read_file=*.go", "-mcp", "local=command", "-model", "old", "-verify-cmd", "go test ./...", "-audit", "audit.jsonl", "-nudges", "off", "-no-verify", "-yes", "-api-key", "transient", "-resume", "old-state"})
	if err != nil {
		t.Fatal(err)
	}
	p := config.DefaultProfile()
	p.Provider.Model = "new"
	prefs := config.Preferences{Version: 1, Profile: &p, Workspace: t.TempDir(), Harness: config.Harness{Nudges: "recovery", MaxSteps: 42}}
	next := relaunchOptions(before, prefs, true)
	after, err := resolveLaunch(launchRequest{options: &next, profile: &p, preferences: prefs})
	if err != nil {
		t.Fatal(err)
	}
	if after.deny != before.deny || after.allow != before.allow || after.mcp != before.mcp || after.verifyCmd != before.verifyCmd || after.auditPath != before.auditPath || !after.noVerify || !after.yes || after.apiKey != before.apiKey {
		t.Fatalf("execution options lost: %+v", after.launchOptions)
	}
	if after.model != "new" || after.maxSteps != 42 || after.nudges != 1 || after.plan || after.resume != "" {
		t.Fatalf("settings or task handoff wrong: %+v", after.launchOptions)
	}
}

func TestFirstRunProfileAndWorkspace(t *testing.T) {
	p := config.DefaultProfile()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	prefs := config.Preferences{Version: 1}
	options, err := parseLaunchOptions([]string{"-tui"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := resolveLaunch(initialTUILaunch(options, prefs, "application.json", true))
	if err != nil {
		t.Fatal(err)
	}
	if o.backendKind != "llama" || o.backendURL != "http://127.0.0.1:8080" || o.workspace != "." || !o.plan {
		t.Fatalf("bad bootstrap: %+v", o)
	}
}

func TestGeneratedProfileKeepsResolvedIdentity(t *testing.T) {
	o, err := resolveLaunch(launchRequest{args: []string{"-tui", "-system-prompt", "CUSTOM", "-user-name", "Alice", "-post-history", "POST"}})
	if err != nil {
		t.Fatal(err)
	}
	p := launchProfile(o, 65536)
	if p.Identity.UserName != "Alice" || p.Identity.SystemPrompt != "CUSTOM" || p.Identity.PostHistory != "POST" {
		t.Fatalf("identity lost: %+v", p.Identity)
	}
}
