package store

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestCallbackAcceptanceSurvivesSnapshotsAndDeduplicates(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	p := &workflow.CallbackState{ID: "wait", StepID: "approval", Channel: "approve", ExpiresAt: expires}
	r := workflow.Run{Status: "waiting", WakeAt: &expires, Steps: []workflow.StepResult{{ID: "approval", Status: "waiting", Callback: p}}}
	_, err = s.db.Exec(`CREATE TRIGGER reject_registration BEFORE INSERT ON wakeups BEGIN SELECT RAISE(ABORT,'wake fault'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	err = w.Save(r)
	if err == nil {
		t.Fatal("partial callback registration succeeded")
	}
	var registered int
	err = s.db.QueryRow("SELECT count(*) FROM callbacks").Scan(&registered)
	if err != nil || registered != 0 {
		t.Fatal(registered, err)
	}
	_, err = s.db.Exec("DROP TRIGGER reject_registration")
	if err != nil {
		t.Fatal(err)
	}
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	delivery := CallbackDelivery{RunID: w.ID(), WaitID: p.ID, Channel: p.Channel, EventID: "event", Payload: json.RawMessage(`{"b":2,"a":9007199254740993}`)}
	duplicate, err := s.AcceptCallback(t.Context(), delivery)
	if err != nil || duplicate {
		t.Fatal(duplicate, err)
	}
	// A stale caller snapshot must not overwrite the accepted event or its due time.
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.Run(w.ID())
	if err != nil || state.Record.Steps[0].Callback.EventID != "event" || state.Record.WakeAt.After(time.Now()) {
		t.Fatal(state, err)
	}
	delivery.Payload = json.RawMessage(`{ "a":9007199254740993, "b":2 }`)
	duplicate, err = s.AcceptCallback(t.Context(), delivery)
	if err != nil || !duplicate {
		t.Fatal(duplicate, err)
	}
	delivery.EventID = "different"
	_, err = s.AcceptCallback(t.Context(), delivery)
	if !errors.Is(err, ErrCallbackConflict) {
		t.Fatal(err)
	}
	delivery.EventID = "event"
	r.Status, r.WakeAt = "running", nil
	r.Steps[0].Status = "running"
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.Run(w.ID())
	if err != nil || state.Record.Steps[0].Callback.EventID != "event" {
		t.Fatal("accepted callback lost before recovery", state, err)
	}
	r.Status, r.WakeAt = "completed", nil
	r.Steps[0].Status = "completed"
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err = s.AcceptCallback(t.Context(), delivery)
	if err != nil || !duplicate {
		t.Fatal(duplicate, err)
	}
	var wakeups int
	err = s.db.QueryRow("SELECT count(*) FROM wakeups").Scan(&wakeups)
	if err != nil || wakeups != 0 {
		t.Fatal(wakeups, err)
	}
}

func TestConcurrentCallbackDeliveryAcceptsOneEvent(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	p := &workflow.CallbackState{ID: "wait", StepID: "approval", Channel: "approve", ExpiresAt: expires}
	err = w.Save(workflow.Run{Status: "waiting", WakeAt: &expires, Steps: []workflow.StepResult{{ID: "approval", Status: "waiting", Callback: p}}})
	if err != nil {
		t.Fatal(err)
	}
	var joined sync.WaitGroup
	deliveries := make(chan error, 16)
	fresh := make(chan bool, 16)
	for range 16 {
		joined.Go(func() {
			duplicate, deliveryErr := s.AcceptCallback(t.Context(), CallbackDelivery{RunID: w.ID(), WaitID: p.ID, Channel: p.Channel, EventID: "same", Payload: json.RawMessage(`true`)})
			deliveries <- deliveryErr
			fresh <- !duplicate
		})
	}
	joined.Wait()
	close(deliveries)
	close(fresh)
	for deliveryErr := range deliveries {
		if deliveryErr != nil {
			t.Fatal(deliveryErr)
		}
	}
	accepted := 0
	for first := range fresh {
		if first {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatal("fresh acceptances", accepted)
	}
}

func TestCallbackRejectionsAndAtomicAcceptance(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	p := &workflow.CallbackState{ID: "wait", StepID: "approval", Channel: "approve", ExpiresAt: expires}
	r := workflow.Run{Status: "waiting", WakeAt: &expires, Steps: []workflow.StepResult{{ID: "approval", Status: "waiting", Callback: p}}}
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	delivery := CallbackDelivery{RunID: w.ID(), WaitID: p.ID, Channel: p.Channel, EventID: "event", Payload: json.RawMessage(`null`)}
	for _, test := range []struct {
		name   string
		change func(*CallbackDelivery)
		want   error
	}{
		{"unknown", func(d *CallbackDelivery) { d.WaitID = "missing" }, ErrCallbackMissing},
		{"channel", func(d *CallbackDelivery) { d.Channel = "wrong" }, ErrCallbackMissing},
		{"event", func(d *CallbackDelivery) { d.EventID = "" }, ErrCallbackInvalid},
		{"payload", func(d *CallbackDelivery) { d.Payload = nil }, ErrCallbackInvalid},
		{"oversized", func(d *CallbackDelivery) {
			d.Payload = json.RawMessage(`"` + strings.Repeat("a", maxCallbackPayload) + `"`)
		}, ErrCallbackInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := delivery
			test.change(&d)
			_, err := s.AcceptCallback(t.Context(), d)
			if !errors.Is(err, test.want) {
				t.Fatal(err)
			}
		})
	}
	_, err = s.db.Exec(`CREATE TRIGGER reject_delivery BEFORE UPDATE ON wakeups BEGIN SELECT RAISE(ABORT,'wake fault'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AcceptCallback(t.Context(), delivery)
	if err == nil {
		t.Fatal("acceptance ignored wake-up fault")
	}
	state, err := s.Run(w.ID())
	if err != nil || state.Record.Steps[0].Callback.EventID != "" {
		t.Fatal("partial acceptance", state, err)
	}
	_, err = s.db.Exec("DROP TRIGGER reject_delivery")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec("UPDATE callbacks SET expires=0 WHERE run_id=?", w.ID())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AcceptCallback(t.Context(), delivery)
	if !errors.Is(err, ErrCallbackClosed) {
		t.Fatal("expired event accepted", err)
	}
	_, err = s.db.Exec("UPDATE callbacks SET expires=? WHERE run_id=?", expires.UnixMilli(), w.ID())
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Enqueue(t.Context(), "recipe.json", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpdateJob(t.Context(), job.ID, func(j *Job) error { j.RunID, j.Status, j.Cancel = w.ID(), "waiting", true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AcceptCallback(t.Context(), delivery)
	if !errors.Is(err, ErrCallbackClosed) {
		t.Fatal("cancellation ignored", err)
	}
	r.Status, r.WakeAt = "cancelled", nil
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AcceptCallback(t.Context(), delivery)
	if !errors.Is(err, ErrCallbackClosed) {
		t.Fatal(err)
	}
}
