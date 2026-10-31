package session

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestSavedFailureOmitsBackendPayloadAndURLSecrets(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("request: %w", &llm.APIError{Status: 401, Body: "private-key"}),
		&url.Error{Op: "Post", URL: "http://user:private-key@host/chat?key=private-key", Err: errors.New("connection refused")},
	} {
		if got := FailureText(err); got == "" || strings.Contains(got, "private-key") {
			t.Fatal(got)
		}
	}
	if got := FailureText(errors.New("bad\x1b[31m" + strings.Repeat("x", 5000))); strings.ContainsRune(got, '\x1b') || len([]rune(got)) > 4097 {
		t.Fatal("unbounded or terminal control in diagnostic")
	}
}
