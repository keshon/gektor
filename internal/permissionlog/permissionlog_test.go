package permissionlog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/permission"
)

func TestWrapGate_DisabledIsPassthrough(t *testing.T) {
	calls := 0
	inner := func(ctx context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error) {
		calls++
		return permission.Allow, nil
	}
	hooked := WrapGate("", "cli", inner)
	if eff, err := hooked(t.Context(), "read_file", "a", json.RawMessage(`{}`)); eff != permission.Allow || err != nil {
		t.Fatalf("passthrough = %v, %v", eff, err)
	}
	if calls != 1 {
		t.Fatalf("inner called %d times", calls)
	}
}

func TestWrapGate_RecordsDecision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	inner := func(ctx context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error) {
		return permission.Deny, errors.New("blocked by operator: use cat instead")
	}
	hooked := WrapGate(path, "tui", inner)
	if _, err := hooked(t.Context(), "run_shell", "rm x", json.RawMessage(`{}`)); err == nil {
		t.Fatal("hook must return the inner result")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec Record
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if rec.Front != "tui" || rec.Tool != "run_shell" || rec.Effect != "deny" || rec.Note != "use cat instead" {
		t.Fatalf("record = %+v", rec)
	}
}

func TestWrapGate_LogFailureNeverBreaksGate(t *testing.T) {
	inner := func(ctx context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error) {
		return permission.Allow, nil
	}
	hooked := WrapGate(filepath.Join(t.TempDir(), "no-such-dir", "a.jsonl"), "cli", inner)
	if eff, err := hooked(t.Context(), "x", "y", json.RawMessage(`{}`)); eff != permission.Allow || err != nil {
		t.Fatalf("logging failure broke the gate: %v, %v", eff, err)
	}
}

func TestWrapGatePreservesContextAndError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	gate := WrapGate(filepath.Join(t.TempDir(), "decisions.jsonl"), "cli", func(received context.Context, _, _ string, _ json.RawMessage) (permission.Effect, error) {
		if received != ctx {
			t.Fatal("gate context was replaced")
		}
		return permission.Deny, received.Err()
	})
	if effect, err := gate(ctx, "read_file", "file.txt", nil); effect != permission.Deny || err != context.Canceled {
		t.Fatalf("gate result changed: %v %v", effect, err)
	}
}
