package engine

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/keshon/gektor/internal/automation"
	"testing"
)

type fakeHandler struct {
	calls         int
	invalid, fail string
}

func (h *fakeHandler) Validate(s automation.Step) error {
	if s.ID == h.invalid {
		return errors.New("invalid")
	}
	return nil
}
func (h *fakeHandler) Run(_ context.Context, s automation.Step) (string, error) {
	h.calls++
	if s.ID == h.fail {
		return "partial", errors.New("failed")
	}
	return "ok", nil
}
func TestValidateAllBeforeEffectsAndStopOnFailure(t *testing.T) {
	d := automation.Definition{Version: 3, Name: "test", Steps: []automation.Step{{ID: "one", Args: json.RawMessage(`{}`)}, {ID: "two", Args: json.RawMessage(`{}`)}, {ID: "three", Args: json.RawMessage(`{}`)}}}
	h := &fakeHandler{invalid: "two"}
	saves := 0
	save := func(automation.Run) error { saves++; return nil }
	if _, err := Run(t.Context(), d, h, save); err == nil || h.calls != 0 || saves != 0 {
		t.Fatal("invalid recipe had effects")
	}
	h = &fakeHandler{fail: "two"}
	var stored automation.Run
	r, err := Run(t.Context(), d, h, func(r automation.Run) error { data, _ := json.Marshal(r); return json.Unmarshal(data, &stored) })
	if err == nil || h.calls != 2 || r.Status != "failed" || stored.Steps[2].Status != "skipped" || stored.Finished == nil || stored.Digest == "" {
		t.Fatalf("bad failure record: %+v %v", stored, err)
	}
	h = &fakeHandler{}
	if _, err := Run(t.Context(), d, h, func(automation.Run) error { return errors.New("disk full") }); err == nil || h.calls != 0 {
		t.Fatal("ran without durable initial record")
	}
}

func TestCompletedStepIsDurableBeforeContinuing(t *testing.T) {
	d := automation.Definition{Version: 3, Name: "durability", Steps: []automation.Step{{ID: "one", Args: json.RawMessage(`{}`)}, {ID: "two", Args: json.RawMessage(`{}`)}}}
	h := &fakeHandler{}
	var stored automation.Run
	_, err := Run(t.Context(), d, h, func(r automation.Run) error {
		if r.Steps[0].Status == "completed" {
			if h.calls != 1 || r.Steps[1].Status != "pending" {
				t.Fatal("next step started before result persisted")
			}
			return errors.New("disk full after effect")
		}
		data, _ := json.Marshal(r)
		return json.Unmarshal(data, &stored)
	})
	if err == nil || h.calls != 1 || stored.Steps[0].Status != "running" {
		t.Fatal("continued after persistence failure", err, h.calls, stored)
	}
}
