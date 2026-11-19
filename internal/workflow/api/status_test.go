package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow/store"
)

func TestStatusRequiresOperatorAndOmitsPayloads(t *testing.T) {
	h, db, _, _ := setup(t)
	_, err := db.Enqueue(t.Context(), "private-path.json", json.RawMessage(recipe), map[string]json.RawMessage{"text": json.RawMessage(`"private-input"`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{"", hook} {
		w := request(h, "GET", "/v1/status", credential, "", "")
		if w.Code != 401 {
			t.Fatal("non-operator read status", w.Code, w.Body)
		}
	}
	w := request(h, "GET", "/v1/status", operator, "", "")
	var status store.Status
	err = json.Unmarshal(w.Body.Bytes(), &status)
	if w.Code != 200 || err != nil || status.Jobs["queued"] != 1 || status.WorkerLeaseHeld || status.LastWorkerTick != nil {
		t.Fatal("incorrect idle-worker observation", w.Code, w.Body, err)
	}
	for _, private := range []string{"private-path", "private-input", "artifact_write", operator, hook} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("status leaked payload", private)
		}
	}
	w = request(h, "POST", "/v1/status", operator, `{}`, "")
	if w.Code != 405 {
		t.Fatal("status accepted a mutation method", w.Code, w.Body)
	}
	err = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	w = request(h, "GET", "/v1/status", operator, "", "")
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"status_unavailable"`) || strings.Contains(w.Body.String(), "database") {
		t.Fatal("storage failure was not reported safely", w.Code, w.Body)
	}
}
