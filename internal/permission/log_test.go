package permission

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogGate_DisabledIsPassthrough(t *testing.T) {
	calls := 0
	inner := func(ctx context.Context, tool, resource string, args json.RawMessage) (Effect, error) {
		calls++
		return Allow, nil
	}
	hooked := LogGate("", "cli", inner)
	if eff, err := hooked(t.Context(), "read_file", "a", json.RawMessage(`{}`)); eff != Allow || err != nil {
		t.Fatalf("passthrough = %v, %v", eff, err)
	}
	if calls != 1 {
		t.Fatalf("inner called %d times", calls)
	}
}

func TestLogGate_RecordsDecision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	inner := func(ctx context.Context, tool, resource string, args json.RawMessage) (Effect, error) {
		return Deny, errors.New("blocked by operator: use cat instead")
	}
	hooked := LogGate(path, "tui", inner)
	if _, err := hooked(t.Context(), "run_shell", "rm x", json.RawMessage(`{}`)); err == nil {
		t.Fatal("hook must return the inner result")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec GateRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if rec.Front != "tui" || rec.Tool != "run_shell" || rec.Effect != "deny" || rec.Note != "use cat instead" {
		t.Fatalf("record = %+v", rec)
	}
}

func TestLogGate_LogFailureNeverBreaksGate(t *testing.T) {
	inner := func(ctx context.Context, tool, resource string, args json.RawMessage) (Effect, error) {
		return Allow, nil
	}
	hooked := LogGate(filepath.Join(t.TempDir(), "no-such-dir", "a.jsonl"), "cli", inner)
	if eff, err := hooked(t.Context(), "x", "y", json.RawMessage(`{}`)); eff != Allow || err != nil {
		t.Fatalf("logging failure broke the gate: %v, %v", eff, err)
	}
}

func TestLogGatePreservesContextAndError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	gate := LogGate(filepath.Join(t.TempDir(), "decisions.jsonl"), "cli", func(received context.Context, _, _ string, _ json.RawMessage) (Effect, error) {
		if received != ctx {
			t.Fatal("gate context was replaced")
		}
		return Deny, received.Err()
	})
	if effect, err := gate(ctx, "read_file", "file.txt", nil); effect != Deny || err != context.Canceled {
		t.Fatalf("gate result changed: %v %v", effect, err)
	}
}
