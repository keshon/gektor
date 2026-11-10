package script

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTransformAndNoHostAPIs(t *testing.T) {
	out, err := Evaluate(t.Context(), Request{Code: `return {title:input.title.trim(), items:input.items.filter(x=>x>1)};`, Input: json.RawMessage(`{"title":" test ","items":[1,2,3]}`)})
	if err != nil || string(out) != `{"title":"test","items":[2,3]}` {
		t.Fatal(string(out), err)
	}
	out, err = Evaluate(t.Context(), Request{Code: `return [typeof process,typeof require,typeof fetch];`, Input: json.RawMessage(`null`)})
	if err != nil || string(out) != `["undefined","undefined","undefined"]` {
		t.Fatal(string(out), err)
	}
}
func TestInterruptInfiniteLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := Evaluate(ctx, Request{Code: `while(true){}`, Input: json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatal(err)
	}
}
