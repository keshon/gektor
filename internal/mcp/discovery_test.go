package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

type contractMCP func(context.Context, string, any, any) error

func (f contractMCP) Call(ctx context.Context, m string, p any, out any) error {
	return f(ctx, m, p, out)
}

func TestMCPCatalogReadsEveryPageAndRejectsCycles(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		calls := 0
		c := contractMCP(func(_ context.Context, m string, p any, out any) error {
			calls++
			result := `{"tools":[{"name":"one"}],"nextCursor":"next"}`
			if calls > 1 && !cycle {
				if p.(map[string]any)["cursor"] != "next" {
					t.Fatal(p)
				}
				result = `{"tools":[{"name":"two"}]}`
			}
			return json.Unmarshal([]byte(result), out)
		})
		defs, err := discover(t.Context(), c)
		if cycle {
			if err == nil || calls != 2 {
				t.Fatal("cursor cycle not bounded", calls, err)
			}
		} else if err != nil || len(defs) != 2 {
			t.Fatal(defs, err)
		}
	}
}
