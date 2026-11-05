package koboldcpp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMaxContextLength_ParsesValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/extra/true_max_context_length" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value": 20480}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "local", nil)
	got, err := c.MaxContextLength(context.Background())
	if err != nil {
		t.Fatalf("MaxContextLength: %v", err)
	}
	if got != 20480 {
		t.Fatalf("got %d, want 20480", got)
	}
}

func TestMaxContextLength_ErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "local", nil)
	if _, err := c.MaxContextLength(context.Background()); err == nil {
		t.Fatal("expected an error for a 404 response, got nil")
	}
}

func TestSupportsVision_KoboldIsAlwaysFalse(t *testing.T) {
	got, err := New("http://127.0.0.1:1", "local", nil).SupportsVision(context.Background())
	if err != nil || got {
		t.Fatalf("kobold SupportsVision = %t, %v", got, err)
	}
}
