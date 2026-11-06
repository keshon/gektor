package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation/api"
	"github.com/keshon/gektor/internal/automation/daemon"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
)

const operator = "operator-test-token-01234567890123456789"
const hook = "webhook-test-token-01234567890123456789"
const recipe = `{"version":3,"name":"Webhook note","inputs":{"text":{"type":"string","required":true}},"steps":[{"id":"save","tool":"artifact_write","args":{"name":"note.txt","text":{"$input":"text"}}}]}`

func TestDiagnosticsCorrelateWithoutLoggingRequestData(t *testing.T) {
	_, db, svc, _ := setup(t)
	var logs bytes.Buffer
	h, err := api.New(t.Context(), db, svc, api.Config{TokenEnv: "TEST_WORKFLOW_OPERATOR", Routes: map[string]api.Route{"note": {RecipePath: "recipe.json", TokenEnv: "TEST_WORKFLOW_HOOK"}}}, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	secret := "private-input-must-not-be-logged"
	r := httptest.NewRequest("POST", "/hooks/note?secret=private-query", strings.NewReader(`{"inputs":{"text":"`+secret+`"}}`))
	r.Header.Set("Authorization", "Bearer "+hook)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "private-delivery-key")
	r.Header.Set("X-Request-ID", "untrusted-request-id")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal(w.Body)
	}
	id := w.Header().Get("X-Request-ID")
	if id == "" || id == "untrusted-request-id" || !strings.Contains(logs.String(), id) {
		t.Fatal(id, logs.String())
	}
	var accepted struct{ ID string }
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), accepted.ID) {
		t.Fatal(logs.String())
	}
	bad := request(h, "POST", "/hooks/note", hook, `{"inputs":{"`+secret+`":"bad"}}`, "")
	if bad.Code != 422 {
		t.Fatal(bad.Code, bad.Body)
	}
	var rejected struct {
		Code      string
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(bad.Body.Bytes(), &rejected); err != nil {
		t.Fatal(err)
	}
	if rejected.Code != "validation_rejected" || rejected.RequestID != bad.Header().Get("X-Request-ID") || !strings.Contains(logs.String(), rejected.RequestID) {
		t.Fatal(bad.Body, logs.String())
	}
	for _, value := range []string{operator, hook, secret, "private-query", "private-delivery-key", "untrusted-request-id"} {
		if strings.Contains(logs.String(), value) {
			t.Fatal("request data leaked", value)
		}
	}
}

func TestServerJoinsHandlerAfterShutdownTimeout(t *testing.T) {
	listener, err := api.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	done := make(chan error, 1)
	go func() {
		done <- api.Serve(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(200) }), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	}()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := http.Client{Timeout: 10 * time.Second}
		r, e := client.Get("http://" + listener.Addr().String())
		if e == nil {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case <-clientDone:
	case <-time.After(8 * time.Second):
		t.Fatal("server did not close timed-out connection")
	}
	select {
	case err := <-done:
		t.Fatalf("returned before handler finished: %v", err)
	default:
	}
	unblock()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not joined")
	}
}

func setup(t *testing.T, sources ...string) (*api.Handler, *store.Store, service.Service, string) {
	t.Helper()
	dir, home := t.TempDir(), t.TempDir()
	source := recipe
	if len(sources) > 0 {
		source = sources[0]
	}
	if err := os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	db, err := store.OpenStore(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc, err := app.NewAutomation(ws, home, permission.Policy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_WORKFLOW_OPERATOR", operator)
	t.Setenv("TEST_WORKFLOW_HOOK", hook)
	h, err := api.New(t.Context(), db, svc, api.Config{TokenEnv: "TEST_WORKFLOW_OPERATOR", Routes: map[string]api.Route{"note": {RecipePath: "recipe.json", TokenEnv: "TEST_WORKFLOW_HOOK"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return h, db, svc, dir
}
func request(h http.Handler, method, path, token, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestIngressBoundariesAndFrozenRecipe(t *testing.T) {
	h, db, svc, dir := setup(t)
	for _, tc := range []struct {
		method, path, token, body string
		status                    int
	}{
		{"POST", "/hooks/note", "", `{"inputs":{"text":"hi"}}`, 401},
		{"POST", "/hooks/note", operator, `{"inputs":{}}`, 401},
		{"GET", "/v1/jobs", hook, "", 401},
		{"POST", "/v1/submit/note", hook, `{"inputs":{}}`, 401},
		{"GET", "/hooks/note", hook, "", 405},
		{"POST", "/hooks/note", hook, `{"inputs":{},"recipe_path":"other.json"}`, 400},
		{"POST", "/hooks/note", hook, `null`, 400},
		{"POST", "/hooks/note", hook, `{"inputs":{}} {}`, 400},
		{"POST", "/hooks/note", hook, `{"inputs":{}}`, 422},
		{"POST", "/hooks/note", hook, `{"inputs":{"text":"` + strings.Repeat("a", 65<<10) + `"}}`, 400},
	} {
		w := request(h, tc.method, tc.path, tc.token, tc.body, "")
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	if err := os.Remove(filepath.Join(dir, "recipe.json")); err != nil {
		t.Fatal(err)
	}
	body := `{"inputs":{"text":"hello"}}`
	first := request(h, "POST", "/hooks/note", hook, body, "delivery-1")
	if first.Code != 202 {
		t.Fatal(first.Code, first.Body)
	}
	repeated := request(h, "POST", "/hooks/note", hook, body, "delivery-1")
	if repeated.Body.String() != first.Body.String() {
		t.Fatal(repeated.Body, first.Body)
	}
	conflict := request(h, "POST", "/hooks/note", hook, `{"inputs":{"text":"changed"}}`, "delivery-1")
	if conflict.Code != 409 {
		t.Fatal(conflict.Code, conflict.Body)
	}
	jobs, err := db.Jobs(t.Context())
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	job, ok, err := db.Claim(t.Context())
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := svc.ExecuteJob(t.Context(), db, job); err != nil {
		t.Fatal(err)
	}
	status := request(h, "GET", "/v1/jobs/"+job.ID, operator, "", "")
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"completed"`) {
		t.Fatal(status.Body)
	}
	if strings.Contains(status.Body.String(), "source") || strings.Contains(status.Body.String(), "hello") {
		t.Fatal("status leaked payload", status.Body)
	}
}

func TestHTTPWorkerLifecycle(t *testing.T) {
	h, db, svc, _ := setup(t)
	listener, err := api.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- api.Serve(ctx, listener, h, func(ctx context.Context) error { return daemon.Serve(ctx, db, svc) })
	}()
	req, _ := http.NewRequest("POST", "http://"+listener.Addr().String()+"/hooks/note", strings.NewReader(`{"inputs":{"text":"live HTTP"}}`))
	req.Header.Set("Authorization", "Bearer "+hook)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ ID string }
	err = json.NewDecoder(res.Body).Decode(&result)
	res.Body.Close()
	if err != nil || res.StatusCode != 202 {
		t.Fatal(res.StatusCode, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		jobs, e := db.Jobs(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		if len(jobs) == 1 && jobs[0].Status == "completed" {
			completed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not join worker")
	}
	if !completed {
		t.Fatal("HTTP job did not execute")
	}
	lease, err := db.DaemonLease()
	if err != nil {
		t.Fatal("worker lease leaked", err)
	}
	lease.Close()
}

func TestListenAndWorkerFailure(t *testing.T) {
	for _, address := range []string{"0.0.0.0:0", "localhost:0", "[::]:0"} {
		if l, e := api.Listen(address); e == nil {
			l.Close()
			t.Fatal("unsafe bind", address)
		}
	}
	l, err := api.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("worker unavailable")
	if err := api.Serve(t.Context(), l, http.NotFoundHandler(), func(context.Context) error { return errors.Join(context.Canceled, failure) }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestWebhookPreservesReviewAndWorkerPolicy(t *testing.T) {
	t.Run("review", func(t *testing.T) {
		h, db, svc, _ := setup(t, strings.Replace(recipe, `"tool":"artifact_write"`, `"tool":"artifact_write","review":true`, 1))
		response := request(h, "POST", "/hooks/note", hook, `{"inputs":{"text":"review me"}}`, "")
		if response.Code != 202 {
			t.Fatal(response.Body)
		}
		job, ok, err := db.Claim(t.Context())
		if !ok || err != nil {
			t.Fatal(err)
		}
		if err := svc.ExecuteJob(t.Context(), db, job); err != nil {
			t.Fatal(err)
		}
		jobs, err := db.Jobs(t.Context())
		if err != nil || jobs[0].Status != "waiting_review" {
			t.Fatal(jobs, err)
		}
		for _, path := range []string{"/v1/jobs/" + job.ID + "/cancel", "/v1/review/" + jobs[0].RunID} {
			if w := request(h, "POST", path, hook, `{}`, ""); w.Code != 401 {
				t.Fatal(w.Code, w.Body)
			}
		}
		if w := request(h, "POST", "/v1/jobs/"+job.ID+"/cancel", operator, `{}`, ""); w.Code != 409 {
			t.Fatal(w.Code, w.Body)
		}
	})
	t.Run("deny", func(t *testing.T) {
		h, db, svc, _ := setup(t)
		w := request(h, "POST", "/hooks/note", hook, `{"inputs":{"text":"denied"}}`, "")
		if w.Code != 202 {
			t.Fatal(w.Body)
		}
		job, ok, err := db.Claim(t.Context())
		if !ok || err != nil {
			t.Fatal(err)
		}
		authorize := svc.AuthorizeResource
		svc.AuthorizeResource = func(ctx context.Context, name, path string, args json.RawMessage) error {
			if name == "artifact_write" {
				return errors.New("denied by worker policy")
			}
			return authorize(ctx, name, path, args)
		}
		if err := svc.ExecuteJob(t.Context(), db, job); err != nil {
			t.Fatal(err)
		}
		jobs, err := db.Jobs(t.Context())
		if err != nil || jobs[0].Status != "failed" {
			t.Fatal(jobs, err)
		}
	})
	t.Run("cancel queued", func(t *testing.T) {
		h, db, _, _ := setup(t)
		request(h, "POST", "/v1/submit/note", operator, `{"inputs":{"text":"cancel"}}`, "")
		jobs, err := db.Jobs(t.Context())
		if err != nil || len(jobs) != 1 {
			t.Fatal(jobs, err)
		}
		w := request(h, "POST", "/v1/jobs/"+jobs[0].ID+"/cancel", operator, "", "")
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body)
		}
		if _, ok, err := db.Claim(t.Context()); err != nil || ok {
			t.Fatal("cancelled job claimable", err)
		}
	})
}
