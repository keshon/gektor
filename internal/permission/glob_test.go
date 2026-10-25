package permission

import "testing"

func TestToolGlobDenyAndExplicitOverride(t *testing.T) {
	p := Default().WithExtra([]Rule{{Tool: "mcp__social__*", Pattern: "*", Effect: Deny}, {Tool: "mcp__social__read", Pattern: "*", Effect: Allow}})
	if got := p.Evaluate("mcp__social__publish", "*"); got != Deny {
		t.Fatal("server deny failed", got)
	}
	if got := p.Evaluate("MCP__SOCIAL__READ", "*"); got != Allow {
		t.Fatal("explicit override failed", got)
	}
	for _, rule := range []Rule{{Tool: "[broken", Pattern: "*", Effect: Deny}, {Tool: "*", Pattern: "[broken", Effect: Deny}} {
		p := Default().WithExtra([]Rule{rule})
		if p.Validate() == nil {
			t.Fatal("malformed pattern accepted", rule)
		}
		if err := Authorize(t.Context(), p, nil, "read_file", "file", nil); err == nil {
			t.Fatal("invalid policy authorized a call")
		}
	}
}
