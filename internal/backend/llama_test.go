package backend

import (
	"context"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelsFindsNestedGGUF(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "publisher", "model")
	os.MkdirAll(nested, 0700)
	os.WriteFile(filepath.Join(nested, "weights.GGUF"), []byte("model"), 0600)
	os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0600)
	files, err := Models(root)
	if err != nil || len(files) != 1 || !strings.HasSuffix(files[0], "weights.GGUF") {
		t.Fatalf("%v %v", files, err)
	}
}
func TestOccupiedPortNeverAttaches(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	l := &Llama{cfg: config.Provider{URL: "http://" + listener.Addr().String()}}
	if err := l.start(context.Background()); err == nil || !strings.Contains(err.Error(), "occupied") {
		t.Fatalf("%v", err)
	}
	if l.child != nil {
		t.Fatal("started a process on occupied port")
	}
}
func TestInstalledLlama(t *testing.T) {
	root := os.Getenv("TARS_LLAMA_TEST_ROOT")
	if root == "" {
		t.Skip("native acceptance opt-in")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + listener.Addr().String()
	listener.Close()
	cfg := config.Provider{Kind: "llama", Ownership: "managed", URL: endpoint, Model: "local", Runtime: filepath.Join(root, "assets", "runtimes", "llamacpp", "llama-server.exe"), ModelFile: filepath.Join(root, "assets", "models", "thinking", "qwen", "Qwen3-1.7B-Q8_0.gguf")}
	server, err := llm.ClientFor("llama", endpoint, "local")
	if err != nil {
		t.Fatal(err)
	}
	server.NoGrammar = true
	l, err := NewLlama(cfg, 2048, server, filepath.Join(root, "var", "thinking-acceptance"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := l.Chat(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "Reply with only Hello. /no_think"}}, MaxTokens: 16})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Message.Content) == "" {
		t.Fatal("empty response")
	}
	t.Logf("native reply: %s", result.Message.Content)
	l.Close()
	probe, err := net.Listen("tcp", strings.TrimPrefix(endpoint, "http://"))
	if err != nil {
		t.Fatal("owned server did not release port", err)
	}
	probe.Close()
}
func TestStartupHintExplainsArchitectureFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("error loading model architecture: unknown model architecture: 'qwen35'\ntry reducing --n-gpu-layers"), 0600); err != nil {
		t.Fatal(err)
	}
	hint := startupHint(path)
	if !strings.Contains(hint, "does not support") || strings.Contains(hint, "insufficient memory") {
		t.Fatal(hint)
	}
}
