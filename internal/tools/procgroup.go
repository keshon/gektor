package tools

import (
	"github.com/keshon/gektor/internal/integrations/process"
	"os/exec"
)

type processGroup struct{ *process.Group }

func newProcessGroup() *processGroup                   { return &processGroup{process.NewGroup()} }
func (g *processGroup) beforeStart(cmd *exec.Cmd)      { g.BeforeStart(cmd) }
func (g *processGroup) afterStart(cmd *exec.Cmd) error { return g.AfterStart(cmd) }
func (g *processGroup) kill(cmd *exec.Cmd) error       { return g.Kill(cmd) }
func (g *processGroup) release()                       { g.Release() }
