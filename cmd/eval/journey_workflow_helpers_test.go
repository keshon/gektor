package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

type journeyWorkflowEvidence struct {
	Runs []journeyWorkflowRun `json:"runs"`
}

type journeyWorkflowRun struct {
	Name       string `json:"name"`
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	ReviewStep string `json:"review_step,omitempty"`
	ReportID   string `json:"report_id,omitempty"`
	ReportText string `json:"report_text,omitempty"`
}

// checkJourneyWorkflow runs only after authoring has ended. Its inputs, approval
// actions and oracle never enter the model workspace or conversation.
func checkJourneyWorkflow(ctx context.Context, ws *workspace.Workspace) (journeyWorkflowEvidence, error) {
	var evidence journeyWorkflowEvidence
	source, err := ws.ReadFile("ledger-review.json")
	if err != nil {
		return evidence, err
	}
	project := make(map[string][]byte)
	for _, path := range []string{"ledger.go", "cmd/ledger/main.go", "go.mod"} {
		data, readErr := ws.ReadFile(path)
		if readErr != nil {
			return evidence, readErr
		}
		project[path] = data
	}
	directory := filepath.Join(filepath.Dir(ws.Root()), "journey-workflow-store")
	svc, err := app.NewWorkflow(ws, directory, permission.Default(), nil)
	if err != nil {
		return evidence, err
	}
	definition, _, err := svc.Compile(ctx, source, "ledger-review.json")
	if err != nil {
		return evidence, fmt.Errorf("compile authored recipe: %w", err)
	}
	if !definition.Inputs["amounts"].Required || !definition.Inputs["report_name"].Required {
		return evidence, fmt.Errorf("amounts and report_name must be required inputs")
	}

	cases := []struct {
		name    string
		amounts []string
		want    string
	}{
		{"fresh-positive", []string{" 12.5 ", "-0.07", "+2", "0.03"}, "1446\n"},
		{"fresh-refund", []string{"-8", "0.01", "2.09", "-0.4"}, "-630\n"},
		{"invalid-amount", []string{"1.00", "2.345"}, ""},
	}
	for _, tc := range cases {
		reportName := tc.name + ".txt"
		amounts, marshalErr := json.Marshal(tc.amounts)
		if marshalErr != nil {
			return evidence, marshalErr
		}
		name, marshalErr := json.Marshal(reportName)
		if marshalErr != nil {
			return evidence, marshalErr
		}
		inputs := map[string]json.RawMessage{"amounts": amounts, "report_name": name}
		result, runErr := svc.RunSource(ctx, source, "ledger-review.json", inputs)
		entry := journeyWorkflowRun{Name: tc.name, RunID: result.RunID, Status: result.Status}
		evidence.Runs = append(evidence.Runs, entry)
		current := &evidence.Runs[len(evidence.Runs)-1]
		if result.RunID == "" {
			return evidence, fmt.Errorf("%s did not start: %v", tc.name, runErr)
		}

		record, artifacts, err := inspectJourneyRun(ctx, ws, directory, result.RunID, "")
		if err != nil {
			return evidence, err
		}
		for _, artifact := range artifacts {
			if artifact.Name == reportName {
				return evidence, fmt.Errorf("%s published report before approval", tc.name)
			}
		}
		review, err := checkJourneyCommands(record, tc.amounts, tc.want == "")
		if err != nil {
			return evidence, fmt.Errorf("%s: %w", tc.name, errors.Join(err, runErr))
		}
		if tc.want == "" {
			if runErr == nil || result.Status != "failed" || review != "" {
				return evidence, fmt.Errorf("invalid input must fail before report review: %s, %v", result.Status, runErr)
			}
			continue
		}
		if runErr != nil || result.Status != "waiting_review" || review == "" {
			return evidence, fmt.Errorf("%s must await report review: %s, %v", tc.name, result.Status, runErr)
		}
		current.ReviewStep = review
		approved, err := svc.Review(ctx, result.RunID, review, true)
		current.Status = approved.Status
		if err != nil || approved.Status != "completed" {
			return evidence, fmt.Errorf("%s approval did not complete: %s, %v", tc.name, approved.Status, err)
		}
		var outputs struct {
			Report workflow.Artifact `json:"report"`
		}
		err = json.Unmarshal(approved.Outputs, &outputs)
		if err != nil {
			return evidence, fmt.Errorf("decode declared report: %w", err)
		}
		if outputs.Report.ID == "" || outputs.Report.Name != reportName || outputs.Report.RunID != result.RunID || outputs.Report.StepID != review {
			return evidence, fmt.Errorf("%s output must identify the approved report artifact", tc.name)
		}
		_, published, err := inspectJourneyRun(ctx, ws, directory, result.RunID, outputs.Report.ID)
		if err != nil {
			return evidence, err
		}
		if len(published) != 1 || published[0].Text != tc.want {
			return evidence, fmt.Errorf("%s report must contain %q; got %+v", tc.name, tc.want, published)
		}
		current.ReportID = outputs.Report.ID
		current.ReportText = published[0].Text
	}
	unchanged, err := ws.ReadFile("ledger-review.json")
	if err != nil || !bytes.Equal(source, unchanged) {
		return evidence, fmt.Errorf("saved recipe changed during repeat executions: %v", err)
	}
	for path, before := range project {
		after, readErr := ws.ReadFile(path)
		if readErr != nil || !bytes.Equal(before, after) {
			return evidence, fmt.Errorf("project file %s changed during workflow executions: %v", path, readErr)
		}
	}
	return evidence, nil
}

type journeyArtifact struct {
	Name string
	Text string
}

func inspectJourneyRun(ctx context.Context, ws *workspace.Workspace, directory, runID, artifactID string) (workflow.Run, []journeyArtifact, error) {
	db, err := store.OpenStore(directory, ws.Root())
	if err != nil {
		return workflow.Run{}, nil, err
	}
	defer db.Close()
	writer, record, err := db.EditRun(runID)
	if err != nil {
		return record, nil, err
	}
	defer writer.Close()
	if artifactID != "" {
		meta, data, err := writer.Artifacts("").Read(ctx, artifactID)
		return record, []journeyArtifact{{Name: meta.Name, Text: string(data)}}, err
	}
	artifacts, err := writer.ListArtifacts()
	if err != nil {
		return record, nil, err
	}
	var names []journeyArtifact
	for _, artifact := range artifacts {
		names = append(names, journeyArtifact{Name: artifact.Name})
	}
	return record, names, nil
}

func checkJourneyCommands(record workflow.Run, amounts []string, invalid bool) (string, error) {
	checked, calculated := false, false
	var checkedAt time.Time
	var calculations []workflow.StepResult
	review := ""
	for _, step := range workflow.ScopedSteps(record) {
		if step.Definition.Tool == "artifact_write" {
			if step.Result.Status == "completed" {
				return "", fmt.Errorf("report artifact written before operator review")
			}
			if step.Result.Status == "waiting_review" && step.Definition.Review {
				if review != "" {
					return "", fmt.Errorf("multiple report reviews")
				}
				review = step.ID
			}
		}
		if step.Definition.Tool != "run_program" || len(step.Result.Args) == 0 {
			continue
		}
		var args struct {
			Program string   `json:"program"`
			Args    []string `json:"args"`
			Workdir string   `json:"workdir"`
		}
		err := json.Unmarshal(step.Result.Args, &args)
		if err != nil {
			return "", err
		}
		if args.Program != "go" || (args.Workdir != "" && args.Workdir != ".") {
			continue
		}
		if slices.Equal(args.Args, []string{"test", "./..."}) && step.Result.Status == "completed" {
			if len(step.Result.Attempts) == 0 || step.Result.Attempts[len(step.Result.Attempts)-1].Finished == nil {
				return "", fmt.Errorf("project checks have no completed execution attempt")
			}
			checked = true
			checkedAt = *step.Result.Attempts[len(step.Result.Attempts)-1].Finished
		}
		want := append([]string{"run", "./cmd/ledger"}, amounts...)
		if slices.Equal(args.Args, want) {
			calculations = append(calculations, step.Result)
		}
	}
	if !checked {
		return "", fmt.Errorf("CLI did not follow successful project checks")
	}
	// Stored steps retain declaration order, not execution order. Dependencies
	// may legitimately schedule a later declaration before an earlier one.
	for _, calculation := range calculations {
		if len(calculation.Attempts) == 0 || calculation.Attempts[0].Started.Before(checkedAt) {
			return "", fmt.Errorf("CLI started before project checks completed")
		}
		wantStatus := "completed"
		if invalid {
			wantStatus = "failed"
		}
		calculated = calculated || calculation.Status == wantStatus
	}
	if !calculated {
		return "", fmt.Errorf("missing successful go test ./... or expected actual CLI invocation")
	}
	return review, nil
}
