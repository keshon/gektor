// Package backend owns optional local thinking servers. Transports stay in llm.
package backend

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/process"
)

type Llama struct {
	mu           sync.Mutex
	cfg          config.Provider
	contextLimit int
	server       *llm.Server
	child        *process.Child
	log          *os.File
	directory    string
	closed       bool
}

func NewLlama(cfg config.Provider, contextLimit int, server *llm.Server, directory string) (*Llama, error) {
	for _, path := range []string{cfg.Runtime, cfg.ModelFile} {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("select a file, not a directory: %s", path)
		}
	}
	if !strings.EqualFold(filepath.Ext(cfg.ModelFile), ".gguf") {
		return nil, fmt.Errorf("managed llama requires a GGUF model")
	}
	return &Llama{cfg: cfg, contextLimit: contextLimit, server: server, directory: directory}, nil
}
func (l *Llama) start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("thinking service is closed")
	}
	if l.child != nil {
		return nil
	}
	u, _ := url.Parse(l.cfg.URL)
	listener, err := net.Listen("tcp", u.Host)
	if err != nil {
		return fmt.Errorf("thinking port occupied; choose another URL: %w", err)
	}
	listener.Close()
	if err = os.MkdirAll(l.directory, 0700); err != nil {
		return err
	}
	log, err := os.CreateTemp(l.directory, "llama-*.log")
	if err != nil {
		return err
	}
	child, err := process.Start(context.Background(), l.cfg.Runtime, filepath.Dir(l.cfg.Runtime), []string{"-m", l.cfg.ModelFile, "--alias", l.cfg.Model, "--host", u.Hostname(), "--port", u.Port(), "-c", strconv.Itoa(l.contextLimit), "-ngl", strconv.Itoa(l.cfg.GPULayers), "--parallel", "1", "-t", "2"}, log)
	if err != nil {
		log.Close()
		return err
	}
	ready, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err = child.Ready(ready, strings.TrimSuffix(l.cfg.URL, "/")); err != nil {
		child.Stop()
		log.Close()
		return fmt.Errorf("thinking startup: %s; %w; log %s", startupHint(log.Name()), err, log.Name())
	}
	l.child, l.log = child, log
	return nil
}
func (l *Llama) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if err := l.start(ctx); err != nil {
		return llm.ChatResponse{}, err
	}
	return l.server.Chat(ctx, req)
}
func (l *Llama) Stream(ctx context.Context, req llm.ChatRequest, delta func(string)) (llm.ChatResponse, error) {
	if err := l.start(ctx); err != nil {
		return llm.ChatResponse{}, err
	}
	return l.server.Stream(ctx, req, delta)
}
func (l *Llama) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.child != nil {
		l.child.Stop()
		l.child = nil
	}
	if l.log != nil {
		l.log.Close()
		l.log = nil
	}
}

// Models discovers GGUF files without assuming a particular vendor directory layout.
func Models(root string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".gguf") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func startupHint(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "server failed to start; check Settings → Model"
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "server failed to start; check Settings → Model"
	}
	offset := info.Size() - 65536
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return "server failed to start"
	}
	data := make([]byte, 65536)
	n, _ := f.Read(data)
	log := string(data[:n])
	if strings.Contains(log, "unknown model architecture") {
		return "installed runtime does not support this model; update the runtime or choose another model in Settings → Model"
	}
	if strings.Contains(log, "out of memory") || strings.Contains(log, "failed to allocate") {
		return "insufficient memory; reduce GPU layers/context or choose a smaller model in Settings → Model"
	}
	return "server failed to start; check Settings → Model"
}
