package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func TestJoinModesPreserveDuplicateAndUnmatchedOrder(t *testing.T) {
	left := []json.RawMessage{json.RawMessage(`{"id":2,"v":"l2"}`), json.RawMessage(`{"id":1,"v":"l1"}`), json.RawMessage(`{"id":3}`)}
	right := []json.RawMessage{json.RawMessage(`{"id":1,"v":"r1"}`), json.RawMessage(`{"id":2,"v":"r2a"}`), json.RawMessage(`{"id":2,"v":"r2b"}`), json.RawMessage(`{"id":4}`)}
	for _, mode := range []string{"inner", "left", "full"} {
		in := joinInput{Left: left, Right: right, LeftKey: "/id", RightKey: "/id", Mode: mode}
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		node := joinDefinition()
		output, err := node.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(mode, err)
		}
		want := `[{"left":{"id":2,"v":"l2"},"right":{"id":2,"v":"r2a"}},{"left":{"id":2,"v":"l2"},"right":{"id":2,"v":"r2b"}},{"left":{"id":1,"v":"l1"},"right":{"id":1,"v":"r1"}}`
		if mode != "inner" {
			want += `,{"left":{"id":3},"right":null}`
		}
		if mode == "full" {
			want += `,{"left":null,"right":{"id":4}}`
		}
		want += `]`
		if output != want {
			t.Fatalf("%s:\ngot  %s\nwant %s", mode, output, want)
		}
		err = workflow.ValidateJSON(json.RawMessage(output), node.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestJoinKeysAreExactAndDoNotCoerceTypes(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{`{"left":[9007199254740993],"right":[9007199254740992,9007199254740993],"left_key":"","right_key":"","mode":"inner"}`, `[{"left":9007199254740993,"right":9007199254740993}]`},
		{`{"left":["1",1,-0],"right":[1,"1",0],"left_key":"","right_key":"","mode":"inner"}`, `[{"left":"1","right":"1"},{"left":1,"right":1},{"left":-0,"right":0}]`},
		{`{"left":[{"a/b":{"~key":""}}],"right":[{"nested":[""]}],"left_key":"/a~1b/~0key","right_key":"/nested/0","mode":"inner"}`, `[{"left":{"a/b":{"~key":""}},"right":{"nested":[""]}}]`},
		{`{"left":[],"right":[1,1],"left_key":"","right_key":"","mode":"full"}`, `[{"left":null,"right":1},{"left":null,"right":1}]`},
		{`{"left":[],"right":[],"left_key":"/id","right_key":"/id","mode":"inner"}`, `[]`},
	} {
		got, err := executeJoin(t.Context(), json.RawMessage(test.raw))
		if err != nil || got != test.want {
			t.Fatal(test.raw, got, test.want, err)
		}
	}
}

func TestJoinRejectsInvalidKeysAndBoundsDuplicateExpansion(t *testing.T) {
	for _, rows := range []string{`[{}]`, `[{"id":null}]`, `[{"id":true}]`, `[{"id":1.0}]`, `[{"id":1e0}]`, `[{"id":[]}]`, `[{"id":{}}]`} {
		for _, side := range []string{"left", "right"} {
			in := joinInput{Left: []json.RawMessage{}, Right: []json.RawMessage{}, LeftKey: "/id", RightKey: "/id", Mode: "inner"}
			var invalid []json.RawMessage
			err := json.Unmarshal([]byte(rows), &invalid)
			if err != nil {
				t.Fatal(err)
			}
			if side == "left" {
				in.Left = invalid
			} else {
				in.Right = invalid
			}
			raw, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			node := joinDefinition()
			_, err = node.Execute(t.Context(), raw)
			var failure *workflow.ExecutionError
			if !errors.As(err, &failure) || failure.Code != "join_failed" || !strings.Contains(err.Error(), side+" row 0") {
				t.Fatal("invalid key was not an attributable operation failure", side, rows, err)
			}
		}
	}
	for _, pointer := range []string{"id", "/bad~", "/bad~2"} {
		in := joinInput{Left: []json.RawMessage{}, Right: []json.RawMessage{}, LeftKey: pointer, Mode: "inner"}
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		_, err = executeJoin(t.Context(), raw)
		if err == nil {
			t.Fatal("empty arrays bypassed pointer validation", pointer)
		}
	}
	in := joinInput{LeftKey: "", RightKey: "", Mode: "inner"}
	for range 101 {
		in.Left = append(in.Left, json.RawMessage(`1`))
		in.Right = append(in.Right, json.RawMessage(`1`))
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeJoin(t.Context(), raw)
	if err == nil || !strings.Contains(err.Error(), "10000 pairs") {
		t.Fatal("duplicate expansion was not bounded", err)
	}
	in.Left, in.Right = in.Left[:100], in.Right[:100]
	raw, err = json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	output, err := executeJoin(t.Context(), raw)
	if err != nil {
		t.Fatal("exact pair limit rejected", err)
	}
	var pairs []joinPair
	err = json.Unmarshal([]byte(output), &pairs)
	if err != nil || len(pairs) != maxJoinRows {
		t.Fatal("exact pair limit truncated", len(pairs), err)
	}

	in.Left = []json.RawMessage{json.RawMessage(`{"id":1,"padding":"` + strings.Repeat("x", 30000) + `"}`)}
	in.LeftKey, in.RightKey = "/id", ""
	raw, err = json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeJoin(t.Context(), raw)
	if err == nil || !strings.Contains(err.Error(), "1048576 output bytes") {
		t.Fatal("output byte growth was not bounded", err)
	}
}

func TestJoinRejectsOversizedKeysAndInputArrays(t *testing.T) {
	in := joinInput{Left: []json.RawMessage{}, Right: []json.RawMessage{}, Mode: "inner"}
	for range maxJoinRows + 1 {
		in.Left = append(in.Left, json.RawMessage(`1`))
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeJoin(t.Context(), raw)
	if err == nil {
		t.Fatal("oversized input array accepted")
	}
	in.Left = []json.RawMessage{json.RawMessage(`"` + strings.Repeat("x", maxJoinKeyBytes) + `"`)}
	raw, err = json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeJoin(t.Context(), raw)
	if err == nil || !strings.Contains(err.Error(), "join key exceeds") {
		t.Fatal("oversized encoded key accepted", err)
	}
}

func TestJoinConcurrentExecutionBorrowsArgumentsReadOnly(t *testing.T) {
	raw := json.RawMessage(`{"left":[1,2],"right":[2,1],"left_key":"","right_key":"","mode":"inner"}`)
	for range 16 {
		t.Run("join", func(t *testing.T) {
			t.Parallel()
			node := joinDefinition()
			output, err := node.Execute(t.Context(), raw)
			if err != nil || output != `[{"left":1,"right":1},{"left":2,"right":2}]` {
				t.Fatal(output, err)
			}
		})
	}
}

func TestJoinCancellationDoesNotBecomeAnOperationFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	node := joinDefinition()
	_, err := node.Execute(ctx, json.RawMessage(`{"left":[],"right":[],"left_key":"","right_key":"","mode":"inner"}`))
	var failure *workflow.ExecutionError
	if !errors.Is(err, context.Canceled) || errors.As(err, &failure) {
		t.Fatal(err)
	}
}
