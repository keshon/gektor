package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/mcp"
)

func TestMCPHeadersReachAllRequestsAndErrorsAreRedacted(t *testing.T) {
	requests := []string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("missing auth", r.Method)
		}
		requests = append(requests, r.Method)
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "bad" {
			w.WriteHeader(401)
			w.Write([]byte("fixture-token"))
			return
		}
		if req.ID == 0 {
			w.WriteHeader(202)
			return
		}
		w.Header().Set("Mcp-Session-Id", "fixture-session")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
	}))
	defer server.Close()
	transport, _, err := mcp.NewHTTP(t.Context(), mcp.HTTPConfig{
		Server:  "fixture",
		URL:     server.URL,
		Client:  server.Client(),
		Headers: map[string]string{"Authorization": "Bearer fixture-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &Session{transport: transport, secrets: []string{"Bearer fixture-token", "fixture-token"}}
	if err = c.Call(context.Background(), "bad", nil, nil); err == nil || strings.Contains(err.Error(), "fixture-token") {
		t.Fatal("unredacted error", err)
	}
	c.Close()
	if len(requests) != 5 || requests[4] != "DELETE" {
		t.Fatal(requests)
	}
}
func TestMCPRejectsCredentialsOnHTTPAndReservedHeaders(t *testing.T) {
	for _, header := range []string{"Host", "Content-Type", "Mcp-Session-Id", "Authorization"} {
		if _, _, e := StartMCPHTTPWithHeaders(context.Background(), "fixture", "http://127.0.0.1:1", map[string]string{header: "secret"}); e == nil {
			t.Fatal("accepted credentials", header)
		}
	}
}
