package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPHTTPRejectsPrivateRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/mcp", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, _, err := StartMCPHTTP(ctx, "redirect", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("private redirect was not refused: %v", err)
	}
}

func TestMCPHTTPFailedDiscoveryClosesSession(t *testing.T) {
	deleted := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted <- r.Header.Get("Mcp-Session-Id")
			w.WriteHeader(204)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "failed-discovery")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{}}`, req.ID)
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/list":
			http.Error(w, "catalog unavailable", 500)
		}
	}))
	defer srv.Close()
	_, c, err := StartMCPHTTP(t.Context(), "failure", srv.URL)
	if err == nil || c != nil {
		t.Fatalf("client=%v error=%v", c, err)
	}
	select {
	case id := <-deleted:
		if id != "failed-discovery" {
			t.Fatal(id)
		}
	default:
		t.Fatal("failed discovery leaked the initialized session")
	}
}
