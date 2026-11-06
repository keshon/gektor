package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	c := &httpMCPClient{server: "fixture", url: server.URL, http: server.Client(), headers: map[string]string{"Authorization": "Bearer fixture-token"}, secrets: []string{"Bearer fixture-token", "fixture-token"}}
	if e := c.call(context.Background(), "initialize", nil, nil); e != nil {
		t.Fatal(e)
	}
	if e := c.notify(context.Background(), map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); e != nil {
		t.Fatal(e)
	}
	if e := c.call(context.Background(), "bad", nil, nil); e == nil || strings.Contains(e.Error(), "fixture-token") {
		t.Fatal("unredacted error", e)
	}
	c.Close()
	if len(requests) != 4 || requests[3] != "DELETE" {
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
