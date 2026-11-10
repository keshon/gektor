// Package api exposes the workspace queue. It never executes recipes in requests.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

// Config contains references to secrets, never secret values. Recipes and their
// dependencies are frozen at startup; changing them requires a service restart.
type Config struct {
	TokenEnv string           `json:"token_env"`
	Routes   map[string]Route `json:"routes"`
}
type Route struct {
	RecipePath string `json:"recipe_path"`
	TokenEnv   string `json:"token_env,omitempty"`
}
type prepared struct {
	path    string
	source  json.RawMessage
	token   [32]byte
	webhook bool
}
type Handler struct {
	db       *store.Store
	svc      service.Service
	token    [32]byte
	routes   map[string]prepared
	requests chan struct{}
	logger   *slog.Logger
}

var routeName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func secret(name string) ([32]byte, error) {
	value := os.Getenv(name)
	if name == "" || len(value) < 32 || strings.TrimSpace(value) != value {
		return [32]byte{}, fmt.Errorf("%s must name an environment variable containing a token of at least 32 bytes without surrounding whitespace", name)
	}
	return sha256.Sum256([]byte(value)), nil
}

func New(ctx context.Context, db *store.Store, svc service.Service, cfg Config, logger *slog.Logger) (*Handler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	token, err := secret(cfg.TokenEnv)
	if err != nil {
		return nil, err
	}
	if len(cfg.Routes) == 0 || len(cfg.Routes) > 64 {
		return nil, fmt.Errorf("API requires 1 to 64 named routes")
	}
	h := &Handler{db: db, svc: svc, token: token, routes: map[string]prepared{}, requests: make(chan struct{}, 32), logger: logger}
	used := map[[32]byte]bool{token: true}
	for name, route := range cfg.Routes {
		if !routeName.MatchString(name) {
			return nil, fmt.Errorf("invalid API route %q", name)
		}
		path, err := svc.WS.Relative(route.RecipePath)
		if err != nil {
			return nil, err
		}
		source, err := svc.SnapshotRecipe(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("route %s: %w", name, err)
		}
		p := prepared{path: path, source: source}
		if route.TokenEnv != "" {
			p.token, err = secret(route.TokenEnv)
			if err != nil {
				return nil, err
			}
			if used[p.token] {
				return nil, fmt.Errorf("route %s must use a distinct webhook token", name)
			}
			used[p.token], p.webhook = true, true
		}
		h.routes[name] = p
	}
	return h, nil
}

func authenticated(r *http.Request, want [32]byte) bool {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimPrefix(value, "Bearer ")))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w *response, status int, code, message string) {
	w.code = code
	reply(w, status, map[string]string{"error": message, "code": code, "request_id": w.id})
}
func summary(j store.Job) map[string]any {
	return map[string]any{"id": j.ID, "status": j.Status, "run_id": j.RunID, "created": j.Created, "cancel_requested": j.Cancel}
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (h *Handler) serveHTTP(w *response, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	webhook := len(parts) == 2 && parts[0] == "hooks"
	var p prepared
	if webhook {
		var ok bool
		p, ok = h.routes[parts[1]]
		w.endpoint = "webhook"
		if ok {
			w.route = parts[1]
		}
		if !ok || !p.webhook || !authenticated(r, p.token) {
			problem(w, 401, "unauthorized", "unauthorized")
			return
		}
	} else if !authenticated(r, h.token) {
		problem(w, 401, "unauthorized", "unauthorized")
		return
	}
	select {
	case h.requests <- struct{}{}:
		defer func() { <-h.requests }()
	default:
		problem(w, 503, "request_limit", "too many active requests")
		return
	}
	if webhook || (len(parts) == 3 && parts[0] == "v1" && parts[1] == "submit") {
		if !webhook {
			w.endpoint = "submit"
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			problem(w, 405, "method_not_allowed", "POST required")
			return
		}
		if !webhook {
			var ok bool
			p, ok = h.routes[parts[2]]
			if ok {
				w.route = parts[2]
			}
			if !ok {
				problem(w, 404, "route_not_found", "unknown route")
				return
			}
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			problem(w, 415, "content_type", "application/json required")
			return
		}
		var body struct {
			Inputs map[string]json.RawMessage `json:"inputs"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			problem(w, 400, "invalid_json", "invalid or oversized JSON request")
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			problem(w, 400, "invalid_json", "expected one JSON object")
			return
		}
		if body.Inputs == nil {
			problem(w, 400, "invalid_inputs", "inputs must be an object")
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if len(key) > 128 || strings.TrimSpace(key) != key {
			problem(w, 400, "invalid_idempotency_key", "invalid Idempotency-Key")
			return
		}
		var keyHash, requestHash string
		if key != "" {
			// Namespace by endpoint and credential; keys are never stored in plaintext.
			keyHash = digest([]byte(r.URL.Path + "\x00" + r.Header.Get("Authorization") + "\x00" + key))
			raw, _ := json.Marshal(struct {
				Path   string
				Source json.RawMessage
				Inputs map[string]json.RawMessage
			}{p.path, p.source, body.Inputs})
			requestHash = digest(raw)
		}
		check := h.svc
		check.ValidateOnly = true
		if _, err := check.RunSource(ctx, p.source, p.path, body.Inputs); err != nil {
			problem(w, 422, "validation_rejected", "recipe inputs or validation rejected; check recipe and service policy")
			return
		}
		job, err := h.db.EnqueueOnce(ctx, p.path, p.source, body.Inputs, keyHash, requestHash)
		if errors.Is(err, store.ErrSubmissionConflict) {
			problem(w, 409, "submission_conflict", "idempotency key already used for a different submission")
			return
		}
		if err != nil {
			problem(w, 503, "queue_unavailable", "queue unavailable")
			return
		}
		w.jobID = job.ID
		reply(w, 202, summary(job))
		return
	}
	if len(parts) >= 2 && parts[0] == "v1" && parts[1] == "jobs" {
		w.endpoint = "jobs"
		if (len(parts) == 2 || len(parts) == 3) && r.Method == http.MethodGet {
			jobs, err := h.db.Jobs(ctx)
			if err != nil {
				problem(w, 503, "queue_unavailable", "queue unavailable")
				return
			}
			values := make([]map[string]any, 0, len(jobs))
			for _, j := range jobs {
				if len(parts) == 3 && parts[2] == j.ID {
					reply(w, 200, summary(j))
					return
				}
				values = append(values, summary(j))
			}
			if len(parts) == 3 {
				problem(w, 404, "job_not_found", "unknown job")
				return
			}
			reply(w, 200, values)
			return
		}
		if len(parts) == 4 && parts[3] == "cancel" && r.Method == http.MethodPost {
			if err := h.db.CancelJob(ctx, parts[2]); err != nil {
				problem(w, 409, "cancel_rejected", "cannot cancel job; inspect it with the CLI")
				return
			}
			reply(w, 202, map[string]bool{"cancel_requested": true})
			return
		}
	}
	problem(w, 404, "endpoint_not_found", "unknown endpoint")
}
