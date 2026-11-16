package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/keshon/gektor/internal/workflow/store"
)

func (h *Handler) acceptCallback(w *response, r *http.Request, parts []string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		problem(w, 405, "method_not_allowed", "POST required")
		return
	}

	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		problem(w, 415, "content_type", "application/json required")
		return
	}

	var body struct {
		EventID string          `json:"event_id"`
		Payload json.RawMessage `json:"payload"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&body)
	if err != nil {
		problem(w, 400, "invalid_json", "invalid or oversized JSON request")
		return
	}
	err = decoder.Decode(new(any))
	if err != io.EOF {
		problem(w, 400, "invalid_json", "expected one JSON object")
		return
	}

	duplicate, err := h.db.AcceptCallback(r.Context(), store.CallbackDelivery{
		RunID:   parts[2],
		WaitID:  parts[3],
		Channel: parts[1],
		EventID: body.EventID,
		Payload: body.Payload,
	})
	switch {
	case errors.Is(err, store.ErrCallbackInvalid):
		problem(w, 400, "invalid_event", "event_id and a JSON payload of at most 32 KiB required; run payload limit is 1 MiB")
	case errors.Is(err, store.ErrCallbackMissing):
		problem(w, 404, "callback_unavailable", "callback unavailable")
	case errors.Is(err, store.ErrCallbackClosed):
		problem(w, 410, "callback_closed", "callback closed or expired")
	case errors.Is(err, store.ErrCallbackConflict):
		problem(w, 409, "callback_conflict", "callback already received a different event")
	case err != nil:
		problem(w, 503, "callback_unavailable", "callback unavailable")
	default:
		reply(w, 202, struct {
			Accepted  bool `json:"accepted"`
			Duplicate bool `json:"duplicate"`
		}{Accepted: true, Duplicate: duplicate})
	}
}
