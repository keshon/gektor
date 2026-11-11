// Package agentrpc adapts prepared run sessions to the stdio RPC protocol.
package agentrpc

// The stdio JSON-RPC server behind -serve: methods run/respond/cancel on
// stdin, event lines plus id-responses on stdout. The protocol is
// documented in docs/reference/rpc.md; the shape below is the whole of it.
//
// One run and one gate may be active at a time. Overlapping requests return
// protocol errors; EOF drains a running task or cancels an unanswered gate.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/permission"
)

// Config carries one prepared launch. Each request copies the complete run config.
type Config struct {
	Run        harness.Config
	Directory  string
	Permission func(agent.Suspender) permission.Gate
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcRunParams struct {
	Task string `json:"task"`

	Images []string `json:"images,omitempty"`
}

type rpcRespondParams struct {
	Answer string `json:"answer"`
}

// Serve runs the request loop until stdin closes. out carries events
// and id-responses; notes (human chatter) go to noteW, never to out.
func Serve(ctx context.Context, d Config, in io.Reader, out io.Writer) error {
	if d.Run.Env.Client == nil || d.Run.Env.WS == nil || d.Directory == "" {
		return fmt.Errorf("stdio RPC requires a prepared environment and task directory")
	}

	var wmu sync.Mutex
	write := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			return
		}

		wmu.Lock()
		defer wmu.Unlock()

		fmt.Fprintf(out, "%s\n", data)
		if f, ok := out.(interface{ Sync() error }); ok {
			_ = f.Sync()
		}
	}
	respond := func(id json.RawMessage, result any, err error) {
		if id == nil {
			id = json.RawMessage("null")
		}

		// Struct, not a map: key order on the wire is id first, stable
		// for readers and log greppers alike.
		rec := struct {
			ID     json.RawMessage `json:"id"`
			Result any             `json:"result,omitempty"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}{ID: id}
		if err != nil {
			rec.Error = &struct {
				Message string `json:"message"`
			}{Message: err.Error()}
		} else {
			rec.Result = result
		}

		write(rec)
	}

	emitter := events.New(&lockedWriter{mu: &wmu, w: out})
	hub := harness.NewGateHub(func(name string, fields map[string]any) {
		emitter.Emit(name, fields)
	})

	var runMu sync.Mutex
	var cancel context.CancelFunc
	var wg sync.WaitGroup
	busy := func() bool {
		runMu.Lock()
		defer runMu.Unlock()

		return cancel != nil
	}
	setCancel := func(fn context.CancelFunc) {
		runMu.Lock()
		defer runMu.Unlock()

		cancel = fn
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	lines := make(chan []byte)
	go func() {
		defer close(lines)

		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)

			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
	}()
	if closer, ok := in.(io.Closer); ok {
		defer closer.Close()
	}

	inputClosed := false
readLoop:
	for {
		var line []byte

		select {
		case <-ctx.Done():
			break readLoop
		case value, ok := <-lines:
			if !ok {
				inputClosed = true
				break readLoop
			}

			line = value
		}

		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			respond(nil, nil, fmt.Errorf("bad request: %w", err))
			continue
		}

		switch req.Method {
		case "run":
			var params rpcRunParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				respond(req.ID, nil, fmt.Errorf("bad run params: %w", err))
				continue
			}

			if busy() {
				respond(req.ID, nil, fmt.Errorf("run already in progress"))
				continue
			}

			runCtx, cancelFn := context.WithCancel(ctx)
			setCancel(cancelFn)
			wg.Add(1)
			go func(id json.RawMessage, params rpcRunParams) {
				defer wg.Done()
				defer cancelFn()

				answer, err := serveRun(runCtx, d, hub, emitter, params.Task, params.Images)
				setCancel(nil)
				if err != nil {
					respond(id, nil, err)
					return
				}

				respond(id, map[string]any{"answer": answer}, nil)
			}(req.ID, params)
		case "respond":
			var params rpcRespondParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				respond(req.ID, nil, fmt.Errorf("bad respond params: %w", err))
				continue
			}

			if err := hub.Respond(params.Answer); err != nil {
				respond(req.ID, nil, err)
				continue
			}

			respond(req.ID, map[string]any{"ok": true}, nil)
		case "cancel":
			runMu.Lock()
			fn := cancel
			runMu.Unlock()
			if fn == nil {
				respond(req.ID, nil, fmt.Errorf("no run in progress"))
				continue
			}

			fn()
			respond(req.ID, map[string]any{"ok": true}, nil)
		default:
			respond(req.ID, nil, fmt.Errorf("unknown method %q (want run, respond or cancel)", req.Method))
		}
	}
	// Stdin closed: no more requests will arrive, but an in-flight run
	// still owns its answer — drain it instead of abandoning it. This is
	// what makes piped one-shots (`printf ... | gektor -serve`) work
	// instead of racing startup against EOF. Exception: a run suspended
	// on a gate can never proceed — its answer was going to arrive on
	// the stdin that just closed — so cancel it instead of hanging.
	// Ctrl+C aborts everything immediately via the signal handler.
	wasPending := hub.HasPending()
	hub.Close()
	if wasPending || ctx.Err() != nil {
		runMu.Lock()
		if cancel != nil {
			cancel()
		}

		runMu.Unlock()
	}

	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if inputClosed {
		return sc.Err()
	}

	return nil
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.w.Write(p)
}

func serveRun(ctx context.Context, d Config, hub *harness.GateHub, emitter *events.Emitter, task string, imagePaths []string) (string, error) {
	if task == "" {
		return "", fmt.Errorf("run needs a task")
	}

	// RPC image paths resolve against the workspace like -image does:
	// a bad path fails the run here, not mid-turn at the backend.
	var images []string

	for _, p := range imagePaths {
		full, err := d.Run.Env.WS.Resolve(p)
		if err != nil {
			return "", fmt.Errorf("image: %w", err)
		}

		images = append(images, full)
	}

	sum := sha1.Sum([]byte(task + time.Now().String()))
	taskID := hex.EncodeToString(sum[:])[:8]
	taskDir := filepath.Join(d.Directory, taskID)
	stateFile := filepath.Join(taskDir, "state.json")
	_ = conversation.WriteSessionTitle(taskDir, conversation.TitleLine(task))

	cfg := d.Run
	cfg.Task, cfg.StateFile, cfg.Images = task, stateFile, images
	cfg.Answer = hub.Suspender()
	cfg.Events = emitter
	if d.Permission != nil {
		cfg.Env.Gate = d.Permission(cfg.Answer)
	}

	emitter.Emit("run_start", map[string]any{"task": task})
	sess, err := harness.New(cfg)
	if err != nil {
		return "", err
	}

	return sess.Run(ctx)
}
