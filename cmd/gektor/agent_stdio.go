package main

import (
	"context"
	"os"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/agentstdio"
	"github.com/keshon/gektor/internal/audit"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/run"
)

func serveAgentLaunch(ctx context.Context, o resolvedLaunch, cfg run.Config) error {
	return agentstdio.Serve(ctx, rpcConfiguration(o, cfg), os.Stdin, os.Stdout)
}

func rpcConfiguration(o resolvedLaunch, cfg run.Config) agentstdio.Config {
	return agentstdio.Config{
		Run:       cfg,
		Directory: gektor.Sub("tasks"),
		Permission: func(answer agent.Suspender) permission.Gate {
			return audit.ContextHook(o.auditPath, "serve", permissionGateContext(o.yes, answer, cfg.Env.WS))
		},
	}
}
