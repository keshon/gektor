package permission

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Authorize was written out twice, once in the agent loop and once in the
// mission check runner. These pin the whole ladder in one place.
func TestAuthorize(t *testing.T) {
	ask := Default()
	asked := 0
	gate := func(context.Context, string, string, json.RawMessage) (Effect, error) {
		asked++
		return Deny, nil
	}
	cases := []struct {
		name   string
		policy Policy
		gate   Gate
		want   bool // allowed
	}{
		{"allow needs no gate", Policy{Rules: []Rule{{Tool: "*", Pattern: "*", Effect: Allow}}}, nil, true},
		{"deny needs no gate", Policy{Rules: []Rule{{Tool: "*", Pattern: "*", Effect: Deny}}}, nil, false},
		{"ask without a gate denies", ask, nil, false},
		{"ask consults the gate", ask, gate, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Default() asks about .env specifically, so the ask cases
			// have to name it or they would pass without consulting.
			resource := "a.txt"
			if tc.name != "allow needs no gate" && tc.name != "deny needs no gate" {
				resource = ".env"
			}
			err := Authorize(context.Background(), tc.policy, tc.gate, "read_file", resource, nil)
			if (err == nil) != tc.want {
				t.Fatalf("Authorize = %v, want allowed=%v", err, tc.want)
			}
		})
	}
	if asked != 1 {
		t.Fatalf("gate consulted %d times, want 1", asked)
	}
}

// A gate that fails to reach an operator blocks. Silently allowing would
// turn a broken prompt into a security hole.
func TestAuthorizeGateErrorBlocks(t *testing.T) {
	boom := errors.New("no operator")
	gate := func(context.Context, string, string, json.RawMessage) (Effect, error) {
		return Allow, boom
	}
	err := Authorize(context.Background(), Default(), gate, "read_file", ".env", nil)
	if err == nil || !strings.Contains(err.Error(), "no operator") {
		t.Fatalf("Authorize = %v, want the gate error", err)
	}
}

// A gate may allow what the policy asked about.
func TestAuthorizeGateCanAllow(t *testing.T) {
	gate := func(context.Context, string, string, json.RawMessage) (Effect, error) {
		return Allow, nil
	}
	if err := Authorize(context.Background(), Default(), gate, "read_file", ".env", nil); err != nil {
		t.Fatalf("Authorize = %v, want allowed", err)
	}
}

func TestDefaultAsksEnv(t *testing.T) {
	p := Default()
	if got := p.Evaluate("read_file", ".env"); got != Ask {
		t.Errorf("read .env = %v, want ask", got)
	}
	if got := p.Evaluate("read_file", "config.go"); got != Allow {
		t.Errorf("read config.go = %v, want allow", got)
	}
}

func TestLastMatchWins(t *testing.T) {
	p := Default().WithExtra([]Rule{{Tool: "read_file", Pattern: ".env", Effect: Deny}})
	if got := p.Evaluate("read_file", ".env"); got != Deny {
		t.Errorf("override = %v, want deny", got)
	}
}

func TestUnknownDefaultsAllow(t *testing.T) {
	p := Default()
	if got := p.Evaluate("run_shell", "go test ./..."); got != Allow {
		t.Errorf("shell = %v, want allow", got)
	}
}

func TestDestructiveShellAsks(t *testing.T) {
	p := Default()
	for _, cmd := range []string{"rm -rf /tmp/x", "del /s /q build", "sudo mkfs.ext4 /dev/sda1", "dd if=/dev/zero of=x", "taskkill /IM chrome.exe /F", `C:\Windows\System32\TASKKILL.EXE /F /IM chrome.exe`, "powershell -Command Stop-Process -Name chrome", "pkill chrome", "killall chrome"} {
		if got := p.Evaluate("run_shell", cmd); got != Ask {
			t.Errorf("shell %q = %v, want ask", cmd, got)
		}
		if got := p.Evaluate("start_background", cmd); got != Ask {
			t.Errorf("background %q = %v, want ask", cmd, got)
		}
	}
	for _, tool := range []string{"run_program", "start_program"} {
		if got := p.Evaluate(tool, "taskkill /IM chrome.exe /F"); got != Ask {
			t.Errorf("%s = %v, want ask", tool, got)
		}
	}
	for _, cmd := range []string{"go test ./...", "npm run dev", "git status"} {
		if got := p.Evaluate("run_shell", cmd); got != Allow {
			t.Errorf("shell %q = %v, want allow", cmd, got)
		}
	}
}

func TestDecide_Answers(t *testing.T) {
	for _, ans := range []string{"y", "Y", "yes", "a", "always"} {
		if eff, err := Decide(ans); eff != Allow || err != nil {
			t.Errorf("Decide(%q) = %v, %v; want allow", ans, eff, err)
		}
	}
	if eff, err := Decide("n"); eff != Deny || err == nil {
		t.Errorf("Decide(n) = %v, %v; want deny", eff, err)
	}
}

func TestDecide_RejectNote(t *testing.T) {
	for _, ans := range []string{"n: use read_file instead", "no: too broad", "deny: not now"} {
		eff, err := Decide(ans)
		if eff != Deny || err == nil {
			t.Fatalf("Decide(%q) = %v, %v; want deny", ans, eff, err)
		}
		if !strings.Contains(err.Error(), strings.TrimSpace(strings.SplitN(ans, ":", 2)[1])) {
			t.Errorf("Decide(%q) dropped the note: %v", ans, err)
		}
	}
	// Bare "n:" with no note denies generically, like "n".
	if _, err := Decide("n:"); err == nil || err.Error() != "blocked by operator" {
		t.Errorf("Decide(n:) = %v, want plain denial", err)
	}
}
