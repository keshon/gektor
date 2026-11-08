package main

import (
	"testing"

	"github.com/keshon/gektor/internal/config"
)

// The TUI writes the harness block; the launch has to read it. Without this
// half the settings are saved faithfully and then ignored, which reads to
// the user as "it reset anyway".
func TestHarnessPreferencesSeedTheLaunch(t *testing.T) {
	o, err := resolveLaunch(launchRequest{
		args:        []string{"-tui"},
		preferences: config.Preferences{Version: 1, Harness: config.Harness{MaxSteps: 42}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.maxSteps != 42 {
		t.Fatalf("max steps = %d, want 42", o.maxSteps)
	}

}

// A flag on the command line is a deliberate instruction for this run, and
// must beat what the last run happened to save.
func TestExplicitFlagsBeatHarnessPreferences(t *testing.T) {
	prefs := config.Preferences{Version: 1, Harness: config.Harness{MaxSteps: 42}}
	o, err := resolveLaunch(launchRequest{
		args:        []string{"-tui", "-max-steps", "7"},
		preferences: prefs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.maxSteps != 7 {
		t.Fatalf("steps = %d", o.maxSteps)
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
	if o.maxSteps != 0 {
		t.Fatalf("steps = %d", o.maxSteps)
	}
}

// A corrupt value must fail the launch rather than silently run with
// something else.
func TestDelegatePreferencesAndFlags(t *testing.T) {
	prefs := config.Preferences{Version: 1, Harness: config.Harness{DelegateSteps: 18, DelegateMaxSteps: 30}}
	for _, tc := range []struct {
		args           []string
		steps, maximum int
		bad            bool
	}{
		{[]string{"-tui"}, 18, 30, false},
		{[]string{"-delegate-steps", "20", "-delegate-max-steps", "40", "-tui"}, 20, 40, false},
		{[]string{"-delegate-steps", "0"}, 0, 0, true},
		{[]string{"-delegate-max-steps", "-1"}, 0, 0, true},
		{[]string{"-delegate-steps", "31"}, 0, 0, true},
	} {
		o, err := resolveLaunch(launchRequest{args: tc.args, preferences: prefs})
		if (err != nil) != tc.bad {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if err == nil && (o.delegateSteps != tc.steps || o.delegateMaxSteps != tc.maximum) {
			t.Fatalf("%v: %d/%d", tc.args, o.delegateSteps, o.delegateMaxSteps)
		}
	}
}
