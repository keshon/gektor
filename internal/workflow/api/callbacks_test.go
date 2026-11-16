package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/api"
)

func TestCallbackIngressUsesChannelCredentialAndOnlyPersists(t *testing.T) {
	_, db, svc, _ := setup(t)
	const credential = "callback-test-token-01234567890123456789"
	t.Setenv("TEST_CALLBACK", credential)
	cfg := api.Config{TokenEnv: "TEST_WORKFLOW_OPERATOR", Callbacks: map[string]api.CallbackRoute{"approval": {TokenEnv: "TEST_CALLBACK"}}}
	h, err := api.New(t.Context(), db, svc, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	p := &workflow.CallbackState{ID: "wait", StepID: "approval", Channel: "approval", ExpiresAt: expires}
	r := workflow.Run{Status: "waiting", WakeAt: &expires, Steps: []workflow.StepResult{{ID: "approval", Status: "waiting", Callback: p}}}
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	path := "/callbacks/approval/" + w.ID() + "/wait"
	body := `{"event_id":"delivery","payload":{"approved":true}}`
	for _, token := range []string{operator, hook, ""} {
		response := request(h, "POST", path, token, body, "")
		if response.Code != 401 {
			t.Fatal(response.Code, response.Body)
		}
	}
	response := request(h, "POST", path, credential, body, "")
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body)
	}
	state, err := db.Run(w.ID())
	if err != nil || state.Record.Status != "waiting" || state.Record.Steps[0].Callback.EventID != "delivery" {
		t.Fatal(state, err)
	}
	response = request(h, "POST", path, credential, body, "")
	if response.Code != 202 || !strings.Contains(response.Body.String(), `"duplicate":true`) {
		t.Fatal(response.Code, response.Body)
	}
	response = request(h, "POST", path, credential, strings.Replace(body, "delivery", "another", 1), "")
	if response.Code != 409 {
		t.Fatal(response.Code, response.Body)
	}
	response = request(h, "POST", strings.Replace(path, "/wait", "/unknown", 1), credential, body, "")
	if response.Code != 404 {
		t.Fatal(response.Code, response.Body)
	}
	response = request(h, "POST", path, credential, `{"event_id":"empty"}`, "")
	if response.Code != 400 {
		t.Fatal(response.Code, response.Body)
	}

	cfg.Callbacks["approval"] = api.CallbackRoute{TokenEnv: "TEST_WORKFLOW_OPERATOR"}
	_, err = api.New(t.Context(), db, svc, cfg, nil)
	if err == nil {
		t.Fatal("operator credential reused for callbacks")
	}
}
