package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

var workflowCommandName = regexp.MustCompile(`^[a-zA-Z0-9_./\\:-]+$`)

// Guidance is separate from the JSON result so pipes remain machine-readable.
func workflowGuidance(note io.Writer, result, directory, workspace, allow, deny string) {
	var summary struct {
		ID     string `json:"run_id"`
		Status string `json:"status"`
	}
	if json.Unmarshal([]byte(result), &summary) != nil || summary.Status != "waiting_review" {
		return
	}
	db, err := store.OpenStore(directory, workspace)
	if err != nil {
		return
	}
	defer db.Close()
	run, err := db.Run(summary.ID)
	if err != nil {
		return
	}
	for _, step := range workflow.ScopedSteps(run.Record) {
		if step.Result.Status != "waiting_review" || !step.Definition.Review || run.Record.Approved[step.ID] {
			continue
		}
		fmt.Fprintf(note, "Run %s is waiting for review of step %q. No need to rerun the recipe.\n", summary.ID, step.ID)
		// Restrict generated shell text to arguments safe in both CMD and
		// PowerShell (and POSIX shells). Unusual values still remain in JSON.
		for _, value := range []string{workspace, allow, deny, summary.ID, step.ID} {
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
		fmt.Fprintln(note, "Inspect: "+executable+" workflow inspect"+base+" "+quote(summary.ID))
		fmt.Fprintln(note, "Approve: "+executable+" workflow review"+base+" -decision approve"+permissions+" "+quote(summary.ID)+" "+quote(step.ID))
		fmt.Fprintln(note, "Reject:  "+executable+" workflow review"+base+" -decision reject"+permissions+" "+quote(summary.ID)+" "+quote(step.ID))
		fmt.Fprintln(note, "Review commands still enforce permissions; add a rule only if a required operation is denied.")
		return
	}
}
