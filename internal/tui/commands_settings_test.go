package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/run"
)

// /max-steps, /budget-think and /nudges all set a model field and rebuild the
// session. The session is built from an env captured once at launch, so all
// three used to rebuild it with the values it started with: the command
// confirmed, restarted the session, and changed nothing.
//
// These assert the env the rebuilt session actually receives.

// runSlash drives a command and accepts any confirmation it raises.
func runSlash(t *testing.T, m *model, text string) *model {
	t.Helper()
	updated, _ := m.command(text)
	m = updated.(*model)
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Destructive {
		// The restart confirmation is a destructive form: pick Restart,
		// then commit it.
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
		next, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(*model)
	}
	return m
}

// sessionModel returns a model whose newSession records the env each
// rebuild is composed from, so the override is observed where it matters.
func sessionModel(t *testing.T) (*model, *[]roles.Env) {
	t.Helper()
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sess = &run.Session{}
	m.limit = 16384
	var built []roles.Env
	m.newSession = func(task, stateFile string, images []string) (*run.Session, error) {
		built = append(built, m.sessionEnv())
		return &run.Session{}, nil
	}
	return m, &built
}

func TestSlashOverridesReachTheRebuiltSession(t *testing.T) {
	cases := []struct {
		command string
		check   func(*testing.T, roles.Env)
	}{
		{"/max-steps 12", func(t *testing.T, e roles.Env) {
			if e.MaxSteps != 12 {
				t.Fatalf("Env.MaxSteps = %d, want 12", e.MaxSteps)
			}
		}},
		{"/max-steps 0", func(t *testing.T, e roles.Env) {
			if e.MaxSteps != unlimitedSteps {
				t.Fatalf("Env.MaxSteps = %d, want %d for no limit", e.MaxSteps, unlimitedSteps)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			m, built := sessionModel(t)
			m = runSlash(t, m, tc.command)
			if len(*built) != 1 {
				t.Fatalf("session rebuilt %d times, want 1", len(*built))
			}
			tc.check(t, (*built)[0])
		})
	}
}

func TestUnlimitedStepsShowsAsNoLimit(t *testing.T) {
	m, _ := sessionModel(t)
	m.maxStepsOverride = noStepLimit
	m.steps = 3
	if got := m.stepBudget(); got != "∞" {
		t.Fatalf("stepBudget() = %q, want a no-limit marker", got)
	}
	if view := m.screenContent(); !strings.Contains(view, "step 3/∞") {
		t.Fatalf("header lost its step badge:\n%s", view)
	}
}

// The context window belongs to the model server; the harness only reports
// it. A command that pretended otherwise is gone, not merely defaulted.
func TestContextWindowIsNotSlashSettable(t *testing.T) {
	for _, row := range commandReference() {
		if strings.HasPrefix(row[0], "/budget-context") {
			t.Fatal("/budget-context is still offered: the window is the server's to report")
		}
	}
	m, _ := sessionModel(t)
	if _, _ = m.command("/budget-context 4096"); m.limit != 16384 {
		t.Fatalf("limit = %d, want the configured window 16384", m.limit)
	}
}
