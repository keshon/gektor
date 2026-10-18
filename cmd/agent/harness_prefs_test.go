package main

import (
	"testing"

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/config"
)

// The TUI writes the harness block; the launch has to read it. Without this
// half the settings are saved faithfully and then ignored, which reads to
// the user as "it reset anyway".
func TestHarnessPreferencesSeedTheLaunch(t *testing.T) {
	o, err := resolveLaunch(launchRequest{
		args:        []string{"-tui"},
		preferences: config.Preferences{Version: 1, Harness: config.Harness{MaxSteps: 42, Nudges: "recovery", ThinkBudget: 4000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.maxSteps != 42 {
		t.Fatalf("max steps = %d, want 42", o.maxSteps)
	}
	if o.nudges != agent.NudgeRecovery {
		t.Fatalf("nudges = %v, want recovery", agent.NudgeLevelName(o.nudges))
	}
	if o.thinkBudget != 4000 {
		t.Fatalf("think budget = %d, want 4000", o.thinkBudget)
	}
}

// A flag on the command line is a deliberate instruction for this run, and
// must beat what the last run happened to save.
func TestExplicitFlagsBeatHarnessPreferences(t *testing.T) {
	prefs := config.Preferences{Version: 1, Harness: config.Harness{MaxSteps: 42, Nudges: "recovery", ThinkBudget: 4000}}
	o, err := resolveLaunch(launchRequest{
		args:        []string{"-tui", "-max-steps", "7", "-nudges", "off", "-reasoning-budget", "99"},
		preferences: prefs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.maxSteps != 7 || o.nudges != agent.NudgeOff || o.thinkBudget != 99 {
		t.Fatalf("saved settings beat the flags: steps=%d nudges=%v think=%d",
			o.maxSteps, agent.NudgeLevelName(o.nudges), o.thinkBudget)
	}
}

// A preferences file written before the harness block existed has no such
// block. Absent must mean the launch default, not zero.
func TestLaunchWithoutHarnessKeepsFlagDefaults(t *testing.T) {
	o, err := resolveLaunch(launchRequest{
		args:        []string{"-tui"},
		preferences: config.Preferences{Version: 1, Theme: "dark"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.maxSteps != 0 || o.thinkBudget != 0 || o.nudges != agent.NudgeFull {
		t.Fatalf("absent harness changed the launch: steps=%d think=%d nudges=%v",
			o.maxSteps, o.thinkBudget, agent.NudgeLevelName(o.nudges))
	}
}

// A corrupt value must fail the launch rather than silently run with
// something else.
func TestUnknownSavedNudgeLevelFailsTheLaunch(t *testing.T) {
	_, err := resolveLaunch(launchRequest{
		args:        []string{"-tui"},
		preferences: config.Preferences{Version: 1, Harness: config.Harness{Nudges: "loud"}},
	})
	if err == nil {
		t.Fatal("an unknown saved nudge level was accepted")
	}
}
