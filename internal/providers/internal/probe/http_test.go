package probe

import (
	"strings"
	"testing"
)

// A trailing slash on a configured backend has to reach the probes too,
// not just the chat call: the probe built "//props" and failed, and the
// caller then ran with no context limit at all.
func TestProbesShareTheBaseNormalisation(t *testing.T) {
	for _, tt := range []struct {
		kind string
		base string
		want string
	}{
		{"llama", "http://127.0.0.1:8080", "/props"},
		{"llama", "http://127.0.0.1:8080/", "/props"},
		{"llama", "http://127.0.0.1:8080///", "/props"},
		{"kobold", "http://127.0.0.1:5001", "/api/v1/model"},
		{"kobold", "http://127.0.0.1:5001/", "/api/v1/model"},
	} {
		got := Endpoint(tt.base, tt.want)
		if strings.Contains(got[len(strings.TrimRight(tt.base, "/")):], "//") {
			t.Errorf("Endpoint(%q, %q) = %q: doubled separator", tt.base, tt.want, got)
		}
		if !strings.HasSuffix(got, tt.want) {
			t.Errorf("Endpoint(%q, %q) = %q", tt.base, tt.want, got)
		}
	}
}
