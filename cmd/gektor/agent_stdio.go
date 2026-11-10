package main

import (
	"context"
	"os"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/agentrpc"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/permissionlog"
	"github.com/keshon/gektor/internal/userdirs"
)

func serveAgentLaunch(ctx context.Context, o resolvedLaunch, cfg harness.Config) error {
	return agentrpc.Serve(ctx, rpcConfiguration(o, cfg), os.Stdin, os.Stdout)
}

func rpcConfiguration(o resolvedLaunch, cfg harness.Config) agentrpc.Config {
	return agentrpc.Config{
		Run:       cfg,
		Directory: userdirs.Sub("tasks"),
		Permission: func(answer agent.Suspender) permission.Gate {
			return permissionlog.WrapGate(o.auditPath, "serve", permissionGateContext(o.yes, answer, cfg.Env.WS))
		},
	}
}
