package llamacpp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSupportsVision_LlamaReadsModalities(t *testing.T) {
	for _, want := range []bool{true, false} {
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/props" {
					http.NotFound(w, r)
					return
				}
				fmt.Fprintf(w, `{"modalities":{"vision":%t}}`, want)
			}))
			defer srv.Close()

			got, err := New(srv.URL, "local", nil).SupportsVision(context.Background())
			if err != nil || got != want {
				t.Fatalf("SupportsVision = %t, %v; want %t", got, err, want)
			}
		})
	}
}
