package background

import (
	"fmt"
	"sort"
	"time"
)

// Process is a copied view of a process owned by this manager.
// Session is the owning state file, not an OS-wide process identity.
type Process struct {
	ID, Directory, Session string
	Command                string
	Args                   []string
	PID                    int
	Started                time.Time
	Exited                 bool
	Stopped                bool
	Error                  string
}

func processSnapshot(proc *bgProc) Process {
	info := proc.info
	info.Args = append([]string(nil), info.Args...)
	info.Exited = proc.exited
	if proc.exitErr != nil {
		info.Error = proc.exitErr.Error()
	}
	return info
}

// List returns only this launch's managed processes, including exited entries.
func (p *Processes) List() []Process {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rows := make([]Process, 0, len(p.procs))
	for _, proc := range p.procs {
		rows = append(rows, processSnapshot(proc))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Started.Equal(rows[j].Started) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Started.Before(rows[j].Started)
	})
	return rows
}

// Inspect returns bounded captured output without consuming notifications or
// the agent's output cursor. Callers cannot inspect arbitrary PIDs.
func (p *Processes) Inspect(id string) (Process, string, error) {
	if p == nil {
		return Process{}, "", fmt.Errorf("no background process manager")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	proc := p.procs[id]
	if proc == nil {
		return Process{}, "", fmt.Errorf("unknown background process id %q", id)
	}
	return processSnapshot(proc), proc.output.String(), nil
}

// Stop is the operator action for an exact ID in this launch's registry.
// Agent tools additionally enforce their own session scope.
func (p *Processes) Stop(id string) error {
	if p == nil {
		return fmt.Errorf("no background process manager")
	}
	if err := p.stop(id); err != nil {
		return err
	}
	p.mu.Lock()
	p.procs[id].info.Stopped = true
	p.mu.Unlock()
	return nil
}
