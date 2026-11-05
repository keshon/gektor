package automation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeHandler struct {
	calls         int
	invalid, fail string
}

func (h *fakeHandler) Validate(s Step) error {
	if s.ID == h.invalid {
		return errors.New("invalid")
	}
	return nil
}
func (h *fakeHandler) Run(_ context.Context, s Step) (string, error) {
	h.calls++
	if s.ID == h.fail {
		return "partial", errors.New("failed")
	}
	return "ok", nil
}
func TestValidateAllBeforeEffectsAndStopOnFailure(t *testing.T) {
	d := Definition{Version: 1, Name: "test", Steps: []Step{{ID: "one", Args: json.RawMessage(`{}`)}, {ID: "two", Args: json.RawMessage(`{}`)}, {ID: "three", Args: json.RawMessage(`{}`)}}}
	h := &fakeHandler{invalid: "two"}
	saves := 0
	save := func(Record) error { saves++; return nil }
	if _, err := Run(t.Context(), d, h, save); err == nil || h.calls != 0 || saves != 0 {
		t.Fatal("invalid recipe had effects")
	}
	h = &fakeHandler{fail: "two"}
	var stored Record
	r, err := Run(t.Context(), d, h, func(r Record) error { data, _ := json.Marshal(r); return json.Unmarshal(data, &stored) })
	if err == nil || h.calls != 2 || r.Status != "failed" || stored.Steps[2].Status != "skipped" || stored.Finished == nil || stored.Digest == "" {
		t.Fatalf("bad failure record: %+v %v", stored, err)
	}
	h = &fakeHandler{}
	if _, err := Run(t.Context(), d, h, func(Record) error { return errors.New("disk full") }); err == nil || h.calls != 0 {
		t.Fatal("ran without durable initial record")
	}
}
