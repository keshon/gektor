package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

var workflowCommandName = regexp.MustCompile(`^[a-zA-Z0-9_./\\:-]+$`)

// Guidance is separate from the JSON result so pipes remain machine-readable.
func workflowGuidance(note io.Writer, result service.Result, directory, workspace, allow, deny string) {
	if result.Status != "waiting_review" && result.Status != "waiting" {
		return
	}
	db, err := store.OpenStore(directory, workspace)
	if err != nil {
		return
	}
	defer db.Close()
	run, err := db.Run(result.RunID)
	if err != nil {
		return
	}
	if result.Status == "waiting" {
		pending := workflow.PendingCallback(run.Record)
		if pending != nil {
			fmt.Fprintf(note, "Run %s waits for callback %s on channel %s until %s. Keep the workspace workflow service running.\n", result.RunID, pending.ID, pending.Channel, pending.ExpiresAt.Format(time.RFC3339))
			return
		}
		deadline := run.Record.WakeAt.Format(time.RFC3339)
		fmt.Fprintf(note, "Run %s waits until %s. Keep the workspace workflow service running to continue.\n", result.RunID, deadline)
		return
	}
	for _, step := range workflow.ScopedSteps(run.Record) {
		if step.Result.Status != "waiting_review" || !step.Definition.Review || run.Record.Approved[step.ID] {
			continue
		}
		fmt.Fprintf(note, "Run %s is waiting for review of step %q. No need to rerun the recipe.\n", result.RunID, step.ID)
		// Restrict generated shell text to arguments safe in both CMD and
		// PowerShell (and POSIX shells). Unusual values still remain in JSON.
		for _, value := range []string{workspace, allow, deny, result.RunID, step.ID} {
			if strings.ContainsAny(value, "\"`$%!\r\n\x00") || strings.HasSuffix(value, `\`) {
				fmt.Fprintln(note, "Use workflow inspect and workflow review with this run ID and the same workspace and permission rules.")
				return
			}
		}
		quote := func(s string) string { return `"` + s + `"` }
		executable := "gektor"
		if len(os.Args) > 0 && workflowCommandName.MatchString(os.Args[0]) {
			executable = os.Args[0]
		}
		base := " -workspace " + quote(workspace)
		permissions := ""
		if allow != "" {
			permissions += " -allow " + quote(allow)
		}
		if deny != "" {
			permissions += " -deny " + quote(deny)
		}
		fmt.Fprintln(note, "Inspect: "+executable+" workflow inspect"+base+" "+quote(result.RunID))
		fmt.Fprintln(note, "Approve: "+executable+" workflow review"+base+" -decision approve"+permissions+" "+quote(result.RunID)+" "+quote(step.ID))
		fmt.Fprintln(note, "Reject:  "+executable+" workflow review"+base+" -decision reject"+permissions+" "+quote(result.RunID)+" "+quote(step.ID))
		fmt.Fprintln(note, "Review commands still enforce permissions; add a rule only if a required operation is denied.")
		return
	}
}
