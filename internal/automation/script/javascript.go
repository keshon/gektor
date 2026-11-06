package script

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dop251/goja"
)

type Request struct {
	Code  string          `json:"code"`
	Input json.RawMessage `json:"input"`
}

func Evaluate(ctx context.Context, r Request) (json.RawMessage, error) {
	if len(r.Code) > 64<<10 || len(r.Input) > 64<<10 {
		return nil, fmt.Errorf("script input exceeds 64 KiB")
	}
	vm := goja.New()
	vm.SetMaxCallStackSize(128)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ctx.Err()) })
	defer stop()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Decode through the JS JSON parser, never expose Go objects or host services.
	if err := vm.Set("__source", string(r.Input)); err != nil {
		return nil, err
	}
	if _, err := vm.RunString(`const input=JSON.parse(__source); delete globalThis.__source;`); err != nil {
		return nil, err
	}
	value, err := vm.RunString(`JSON.stringify((function(input){"use strict";` + r.Code + `})(input))`)
	if err != nil {
		return nil, fmt.Errorf("javascript: %w", err)
	}
	if goja.IsUndefined(value) {
		return nil, fmt.Errorf("script must return a JSON value")
	}
	out := json.RawMessage(value.String())
	if len(out) > 1<<20 {
		return nil, fmt.Errorf("script output exceeds 1 MiB")
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("script returned invalid JSON")
	}
	return out, nil
}
