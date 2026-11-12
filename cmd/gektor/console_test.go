package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/permission"
)

func TestPermissionGateSessionScopeAndIsolation(t *testing.T) {
	for _, answer := range []string{"y", "a", "always"} {
		t.Run(answer, func(t *testing.T) {
			prompts := 0
			suspend := func(ctx context.Context, req agent.SuspendRequest) (agent.SuspendReply, error) {
				prompts++
				if !strings.Contains(req.Prompt, `Allow for this session on "ssh://root@host:22"`) {
					t.Fatalf("missing exact session target: %s", req.Prompt)
				}
				return agent.SuspendReply{Answer: answer}, nil
			}
			gate := permissionGateContext(false, suspend, nil)
			for _, command := range []string{"df -h", "ls /opt"} {
				effect, err := gate(t.Context(), "ssh_exec", "ssh://root@host:22 "+command, json.RawMessage(`{}`))
				if err != nil || effect != permission.Allow {
					t.Fatalf("%v, %v", effect, err)
				}
			}
			want := 1
			if answer == "y" {
				want = 2
			}
			if prompts != want {
				t.Fatalf("prompts = %d, want %d", prompts, want)
			}

			fresh := permissionGateContext(false, suspend, nil)
			_, err := fresh(t.Context(), "ssh_exec", "ssh://root@host:22 df -h", nil)
			if err != nil {
				t.Fatal(err)
			}
			if prompts != want+1 {
				t.Fatal("new session reused old grant")
			}
		})
	}
}

func TestPermissionAutoDenyNeverPrompts(t *testing.T) {
	gate := permissionGateContext(true, nil, nil)
	effect, err := gate(t.Context(), "ssh_exec", "ssh://root@host:22 df -h", nil)
	if err != nil || effect != permission.Deny {
		t.Fatalf("%v, %v", effect, err)
	}
}

func TestCancelledPermissionDoesNotRememberGrant(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	prompts := 0
	suspend := func(ctx context.Context, req agent.SuspendRequest) (agent.SuspendReply, error) {
		prompts++
		cancel()
		return agent.SuspendReply{Answer: "a"}, nil
	}
	gate := permissionGateContext(false, suspend, nil)
	effect, err := gate(ctx, "ssh_exec", "ssh://root@host:22 df -h", nil)
	if err == nil || effect != permission.Deny {
		t.Fatalf("cancelled approval = %v, %v", effect, err)
	}
	_, err = gate(t.Context(), "ssh_exec", "ssh://root@host:22 df -h", nil)
	if err != nil {
		t.Fatal(err)
	}
	if prompts != 2 {
		t.Fatal("cancelled grant was remembered")
	}
}
