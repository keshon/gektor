package backend

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/tars/internal/config"
)

// The test binary doubles as a lightweight native server. This exercises the
// real process owner and readiness path without model weights or a GPU.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-m" {
		var host, port string
		for i := 1; i+1 < len(os.Args); i++ {
			switch os.Args[i] {
			case "--host":
				host = os.Args[i+1]
			case "--port":
				port = os.Args[i+1]
			}
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
		_ = http.ListenAndServe(net.JoinHostPort(host, port), mux)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestThinkingServerRestartsOnlyAfterOwnedChildExits(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + listener.Addr().String()
	listener.Close()
	l := &Llama{cfg: config.Provider{Runtime: binary, ModelFile: filepath.Join(t.TempDir(), "unused.gguf"), URL: endpoint}, directory: t.TempDir(), contextLimit: 1000}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := l.start(ctx); err != nil {
		t.Fatal(err)
	}
	first, oldLog := l.child, l.log
	if err := l.start(ctx); err != nil || l.child != first {
		t.Fatal("live child replaced", err)
	}
	first.Stop()
	if err := l.start(ctx); err != nil {
		t.Fatal("dead child did not restart", err)
	}
	if l.child == first || l.child.Exited() {
		t.Fatal("dead child retained")
	}
	if _, err := oldLog.WriteString("closed?"); err == nil {
		t.Fatal("previous log handle leaked")
	}
	l.Close()
	if err := l.start(ctx); err == nil || l.child != nil {
		t.Fatal("closed owner restarted")
	}
}
