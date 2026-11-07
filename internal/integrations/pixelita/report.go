package pixelita

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	imagekit "github.com/keshon/pixelita"
)

type imageOutcome struct {
	failed, blocked, publicationFailed bool
	err                                error
}

func (t Operation) evaluateReport(in Arguments, rep *imagekit.Report, sourceErr error) imageOutcome {
	failed := rep.Summary.Failed > 0
	blocked := failed
	for _, item := range rep.Items {
		if item.Code == "unsupported_metadata" {
			blocked = true
		}
	}
	if len(in.Widths) > 0 && rep.Summary.Skipped > 0 {
		blocked = true
	}
	if t.Operation == "variants" && rep.Summary.Changed != len(in.Paths)*len(in.Widths) {
		blocked = true
	}
	var operationErr error
	if sourceErr != nil {
		failed = true
		blocked = true
		operationErr = sourceErr
	}
	if blocked {
		reason := "requested outputs were not produced"
		for _, it := range rep.Items {
			if it.Status == "failed" || (!failed && it.Status == "skipped") {
				reason = it.Code + ": " + it.Reason
				break
			}
		}
		if operationErr == nil {
			operationErr = fmt.Errorf("image %s blocked: %s; no workspace outputs written", t.Operation, reason)
		}
	}
	return imageOutcome{failed: failed, blocked: blocked, err: operationErr}
}

func (t Operation) formatReport(in Arguments, rep *imagekit.Report, staged stagedImages, outcome imageOutcome) (string, error) {
	for i := range rep.Items {
		it := &rep.Items[i]
		if outcome.blocked && t.Operation == "variants" {
			it.Written = false
			it.Output = ""
			if it.Status == "done" {
				it.Status = "skipped"
				it.Code = "batch_not_published"
				it.Reason = "batch was not published; see operation error"
				if outcome.publicationFailed {
					it.Status = "not_published"
					it.Code = "publication_failed"
					it.Reason = outcome.err.Error()
				}
			}
		}
		if path, ok := staged.names[it.Path]; ok {
			it.Path = filepath.ToSlash(path)
		}
		if it.Output != "" {
			if t.Operation == "variants" {
				it.Output = filepath.ToSlash(filepath.Join(in.OutDir, filepath.Base(it.Output)))
			} else {
				it.Output = filepath.Base(it.Output)
			}
		}
	}
	rep.Finish()
	status := "inspected"
	if t.Operation == "compare" {
		status = "compared"
	}
	if t.Operation == "variants" {
		status = "written"
	}
	if outcome.blocked {
		status = "blocked"
	} else if t.Operation == "preview" {
		status = "previewed"
	}
	if outcome.failed {
		status = "failed"
	}
	recovery := ""
	if outcome.blocked {
		recovery = "Report the item restriction; do not retry unchanged arguments or invent unsupported recovery commands."
		for _, item := range rep.Items {
			if item.Code == "unsupported_metadata" {
				recovery = "Capability limitation: no supported workaround is exposed for this metadata. keep/apply/retry do not help. Report the blocker; do not invent conversion commands or strip profiles automatically."
				break
			}
		}
	}
	if outcome.failed {
		recovery = "Report the operation error. For an existing destination, choose a fresh output directory only if authorized; do not overwrite it or retry unchanged arguments."
	}
	failureStage, errorText := "", ""
	if outcome.err != nil {
		errorText = outcome.err.Error()
		failureStage = "processing"
	}
	if outcome.publicationFailed {
		failureStage = "publication"
		recovery = "Encoding succeeded; publication failed. Read the operation error and preserve existing files."
		if errors.Is(outcome.err, os.ErrExist) {
			recovery += " Choose a fresh output directory only if authorized."
		}
	}
	// Native clients see operation outcomes, rather than CLI accounting labels
	// such as skipped/headers_only for a successful inspection.
	summary := map[string]int{"files": len(rep.Items)}
	for i := range rep.Items {
		it := &rep.Items[i]
		switch {
		case t.Operation == "inspect" && it.Code == "headers_only":
			it.Status = "inspected"
		case it.Status == "would":
			it.Status = "previewed"
		case it.Status == "done":
			it.Status = "written"
			if t.Operation == "compare" {
				it.Status = "compared"
			}
		case it.Code == "unsupported_metadata":
			it.Status = "blocked"
		}
		summary[string(it.Status)]++
	}
	data, err := json.Marshal(struct {
		*imagekit.Report
		Schema       string           `json:"schema"`
		Tool         string           `json:"tool"`
		Summary      map[string]int   `json:"summary"`
		Status       string           `json:"status"`
		Recovery     string           `json:"recovery,omitempty"`
		Sources      []sourceEvidence `json:"sources"`
		FailureStage string           `json:"failure_stage,omitempty"`
		Error        string           `json:"error,omitempty"`
	}{rep, "gektor.images.v1", t.Name(), summary, status, recovery, staged.sources, failureStage, errorText})
	if err != nil {
		return "", err
	}
	return string(data), outcome.err
}
