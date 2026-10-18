package agent

import (
	"encoding/json"
	"testing"
)

// The permission resource is read from the argument shape, not the tool
// name, so a new tool is covered by using path, command or url rather than
// by being added to a switch here. These pin that shape.
func TestToolResourceReadsTheArgumentShape(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"file tool", `{"path":"a/b.go"}`, "a/b.go"},
		{"shell", `{"command":"go test ./..."}`, "go test ./..."},
		{"background shell", `{"command":"serve","name":"web"}`, "serve"},
		{"url", `{"url":"https://example.com/x"}`, "https://example.com/x"},
		{"rename shows both ends", `{"from":"old.txt","to":"new.txt"}`, "old.txt->new.txt"},
		{"path wins over command", `{"path":"a.go","command":"rm a.go"}`, "a.go"},
		{"unparseable arguments", `{`, ""},
		{"nothing recognisable", `{"depth":3}`, `{"depth":3}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolResource(json.RawMessage(tc.args)); got != tc.want {
				t.Fatalf("toolResource(%s) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
