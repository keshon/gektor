package permission

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestSessionApprovalTargets(t *testing.T) {
	tests := []struct {
		tool, resource, nextTool, nextResource string
		allowed                                bool
	}{
		{"ssh_exec", "ssh://root@host:22 df -h", "ssh_exec", "ssh://root@host:22 ls /opt", true},
		{"ssh_exec", "ssh://root@host:22 df -h", "ssh_exec", "ssh://root@other:22 df -h", false},
		{"ssh_exec", "ssh://root@host:22 df -h", "ssh_exec", "ssh://user@host:22 df -h", false},
		{"ssh_exec", "ssh://root@host:22 df -h", "ssh_exec", "ssh://root@host:23 df -h", false},
		{"ssh_exec", "ssh://root@host:22 df -h", "ssh_upload", "ssh://root@host:22", false},
		{"write_file", "notes.txt", "write_file", "notes.txt", true},
		{"write_file", "notes.txt", "write_file", "other.txt", false},
		{"write_file", "notes.txt", "read_file", "notes.txt", false},
		{"ssh_upload", "ssh://root@host:22/a%20b", "ssh_upload", "ssh://root@host:22/c", false},
		{"webfetch", "https://example.com/a", "webfetch", "https://example.com/b", false},
		{"run_shell", "git status", "run_shell", "git reset --hard", false},
		{"custom_tool", "opaque target", "custom_tool", "opaque target", true},
		{"custom_tool", "opaque target", "custom_tool", "opaque other", false},
		{"webfetch", "https://host/a b", "webfetch", "https://host/a c", false},
		{"write_file", "a b.txt", "write_file", "a c.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.tool+"/"+tt.nextResource, func(t *testing.T) {
			var approvals Approvals
			approvals.Remember(tt.tool, tt.resource)
			got := approvals.Allows(tt.nextTool, tt.nextResource)
			if got != tt.allowed {
				t.Fatalf("Allows(%q, %q) = %v", tt.nextTool, tt.nextResource, got)
			}
			if approvals.Count() != 1 {
				t.Fatal("missing grant")
			}
			approvals.Reset()
			if approvals.Allows(tt.tool, tt.resource) {
				t.Fatal("reset retained grant")
			}
		})
	}
}

func TestFullAccessStillObeysPolicyForEveryResource(t *testing.T) {
	var approvals Approvals
	approvals.Remember("write_file", "a")
	approvals.SetFullAccess(true)
	gate := func(ctx context.Context, tool, resource string, args json.RawMessage) (Effect, error) {
		if approvals.Allows(tool, resource) {
			return Allow, nil
		}
		return Deny, nil
	}
	policy := Policy{Rules: []Rule{
		{Tool: "*", Pattern: "*", Effect: Ask},
		{Tool: "*", Pattern: "secret.txt", Effect: Deny},
	}}
	err := Authorize(t.Context(), policy, gate, "write_file", "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = Authorize(t.Context(), policy, gate, "write_file", "secret.txt", nil)
	if err == nil {
		t.Fatal("full access overrode deny on another resource")
	}

	approvals.SetFullAccess(false)
	if approvals.Allows("write_file", "a") || approvals.Count() != 0 {
		t.Fatal("returning to Ask kept grants")
	}
	approvals.SetFullAccess(true)
	approvals.Reset()
	if approvals.FullAccess() {
		t.Fatal("new session inherited full access")
	}
}

func TestSessionApprovalsConcurrentUse(t *testing.T) {
	var approvals Approvals
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 100 {
				approvals.Remember("tool", "target")
				approvals.Allows("tool", "target")
				approvals.Count()
				approvals.FullAccess()
				approvals.SetFullAccess(true)
				approvals.Reset()
			}
		})
	}
	workers.Wait()
}
