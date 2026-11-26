package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
)

type questionCheckpoint struct {
	SourceStateFile string          `json:"source_state_file"`
	Path            string          `json:"path"`
	CallID          string          `json:"call_id"`
	Question        string          `json:"question"`
	SHA256          string          `json:"sha256"`
	Report          agent.RunReport `json:"interrupted_report"`
}

// interruptJourneyAtQuestion copies the actual pending on-disk checkpoint, then
// cancels and joins the run. This measures checkpoint recovery, not an OS crash.
// A missing question or failed checkpoint is an error, never a synthesized pause.
func interruptJourneyAtQuestion(ctx context.Context, cfg harness.Config, priorHistory []llm.Message, checkpointPath string) (questionCheckpoint, error) {
	var checkpoint questionCheckpoint
	source, target, err := journeyStatePaths(cfg, checkpointPath)
	if err != nil {
		return checkpoint, err
	}
	cfg.StateFile, cfg.Env.StateDir = source, ""
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var captureErr error
	cfg.Answer = func(callCtx context.Context, request agent.SuspendRequest) (agent.SuspendReply, error) {
		defer cancel()
		if request.Kind != agent.SuspendAsk {
			captureErr = fmt.Errorf("journey reached %s instead of a pending question", request.Kind)
			return agent.SuspendReply{}, captureErr
		}
		snapshot, err := conversation.Load(source)
		if err != nil {
			captureErr = fmt.Errorf("load pending journey checkpoint: %w", err)
			return agent.SuspendReply{}, captureErr
		}
		callID, question, pending := agent.PausedOnQuestion(nil, snapshot.History)
		if !pending || callID == "" || callID != agent.ToolCallID(callCtx) {
			captureErr = fmt.Errorf("saved journey checkpoint does not contain the active pending question")
			return agent.SuspendReply{}, captureErr
		}
		err = snapshot.Save(target)
		if err != nil {
			captureErr = fmt.Errorf("save pending journey checkpoint: %w", err)
			return agent.SuspendReply{}, captureErr
		}
		digest, err := fileDigest(target)
		if err != nil {
			captureErr = fmt.Errorf("hash pending journey checkpoint: %w", err)
			return agent.SuspendReply{}, captureErr
		}
		checkpoint = questionCheckpoint{SourceStateFile: source, Path: target, CallID: callID, Question: question, SHA256: digest}
		return agent.SuspendReply{}, context.Canceled
	}
	session, err := harness.New(cfg)
	if err != nil {
		return checkpoint, err
	}
	if len(priorHistory) == 0 {
		_, err = session.Run(runCtx)
	} else {
		_, err = session.Continue(runCtx, priorHistory, cfg.Task, cfg.Images...)
	}
	checkpoint.Report = session.Report()
	if captureErr != nil {
		return checkpoint, captureErr
	}
	if ctx.Err() != nil {
		return checkpoint, ctx.Err()
	}
	if checkpoint.Path == "" {
		return checkpoint, errors.Join(fmt.Errorf("journey never reached a saved pending question"), err)
	}
	if !errors.Is(err, context.Canceled) || checkpoint.Report.Outcome != "cancelled" {
		return checkpoint, errors.Join(fmt.Errorf("journey did not stop after checkpoint capture: outcome=%s", checkpoint.Report.Outcome), err)
	}
	return checkpoint, nil
}

// resumeJourneyQuestion preserves the captured checkpoint and cancelled archive.
// The caller supplies a separate destination, operator answer, and new task.
// Model and verification failures are returned with the measured report intact.
func resumeJourneyQuestion(ctx context.Context, cfg harness.Config, checkpoint questionCheckpoint) (string, agent.RunReport, error) {
	destination, preserved, err := journeyStatePaths(cfg, checkpoint.Path)
	if err != nil {
		return "", agent.RunReport{}, err
	}
	if sameJourneyPath(destination, checkpoint.SourceStateFile) {
		return "", agent.RunReport{}, fmt.Errorf("resume destination would replace the interrupted journey archive")
	}
	if strings.TrimSpace(cfg.ResumeAnswer) == "" {
		return "", agent.RunReport{}, fmt.Errorf("journey resume requires an explicit operator answer")
	}
	digest, err := fileDigest(preserved)
	if err != nil {
		return "", agent.RunReport{}, err
	}
	if digest != checkpoint.SHA256 {
		return "", agent.RunReport{}, fmt.Errorf("pending journey checkpoint changed before resume")
	}
	snapshot, err := conversation.Load(preserved)
	if err != nil {
		return "", agent.RunReport{}, err
	}
	callID, question, pending := agent.PausedOnQuestion(nil, snapshot.History)
	if !pending || callID != checkpoint.CallID || question != checkpoint.Question {
		return "", agent.RunReport{}, fmt.Errorf("journey checkpoint no longer contains the recorded pending question")
	}
	err = snapshot.Save(destination)
	if err != nil {
		return "", agent.RunReport{}, err
	}
	cfg.StateFile, cfg.Env.StateDir = destination, ""
	session, err := harness.New(cfg)
	if err != nil {
		return "", agent.RunReport{}, err
	}
	answer, err := session.Resume(ctx, snapshot.History, cfg.Task, cfg.Images...)
	return answer, session.Report(), err
}

func journeyStatePaths(cfg harness.Config, checkpointPath string) (string, string, error) {
	if strings.TrimSpace(cfg.StateFile) == "" || strings.TrimSpace(checkpointPath) == "" {
		return "", "", fmt.Errorf("journey needs separate run and checkpoint state files")
	}
	stateFile := cfg.StateFile
	if cfg.Env.StateDir != "" {
		stateFile = filepath.Join(cfg.Env.StateDir, stateFile)
	}
	source, err := filepath.Abs(stateFile)
	if err != nil {
		return "", "", err
	}
	target, err := filepath.Abs(checkpointPath)
	if err != nil {
		return "", "", err
	}
	if sameJourneyPath(source, target) {
		return "", "", fmt.Errorf("journey checkpoint must not replace the run archive")
	}
	return source, target, nil
}

func sameJourneyPath(first, second string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(first, second)
	}
	return first == second
}
