package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/tars/internal/speech"
)

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

func TestRecognitionRestartsOnlyAfterOwnedChildExits(t *testing.T) {
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
	log, err := os.Create(filepath.Join(t.TempDir(), "speech.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	owner, closeOwner := context.WithCancel(context.Background())
	defer closeOwner()
	m := &Manager{ctx: owner, log: log, config: speech.Config{Listening: speech.Listening{Ownership: "managed", Binary: binary, URL: endpoint, Model: "unused"}}}
	defer func() {
		if m.asr != nil {
			m.asr.Stop()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureRecognition(ctx); err != nil {
		t.Fatal(err)
	}
	first := m.asr
	if err := m.ensureRecognition(ctx); err != nil || m.asr != first {
		t.Fatal("live child replaced", err)
	}
	first.Stop()
	if err := m.ensureRecognition(ctx); err != nil {
		t.Fatal("dead recognizer did not restart", err)
	}
	if m.asr == first || m.asr.Exited() {
		t.Fatal("dead recognizer retained")
	}
	closeOwner()
	m.asr.Stop()
	if err := m.ensureRecognition(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("closed owner restarted", err)
	}
}
