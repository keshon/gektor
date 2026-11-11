package llm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
	"github.com/keshon/gektor/internal/llm/responses"
)

// cancellationClient is a test-only contract shared by both real protocols.
type cancellationClient interface {
	llm.Client
	llm.Streamer
}

// blockedResponse holds an HTTP body open until cancellation. The test owns
// release and waits for both its client goroutine and the HTTP handler to exit.
type blockedResponse struct {
	entered  chan struct{}
	finished chan struct{}
	release  chan struct{}
	once     sync.Once
	requests atomic.Int32
}

func newBlockedResponse() *blockedResponse {
	return &blockedResponse{
		entered:  make(chan struct{}, 1),
		finished: make(chan struct{}, 1),
		release:  make(chan struct{}),
	}
}

func (b *blockedResponse) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	b.requests.Add(1)
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)
	writer.(http.Flusher).Flush()
	b.entered <- struct{}{}

	select {
	case <-request.Context().Done():
	case <-b.release:
	}
	b.finished <- struct{}{}
}

func (b *blockedResponse) unblock() {
	b.once.Do(func() { close(b.release) })
}

func contractToken(context.Context) (string, error) {
	return "local-contract-token", nil
}

func TestChatCompletionsCancellationContract(t *testing.T) {
	endpoint := newBlockedResponse()
	server := httptest.NewServer(endpoint)
	defer server.Close()
	defer endpoint.unblock()

	client := chatcompletions.NewOpenAIClient(server.URL, "contract")
	assertCancellationContract(t, client, endpoint)
}

func TestResponsesCancellationContract(t *testing.T) {
	endpoint := newBlockedResponse()
	server := httptest.NewServer(endpoint)
	defer server.Close()
	defer endpoint.unblock()

	client := responses.New(server.URL, "contract", "", contractToken)
	assertCancellationContract(t, client, endpoint)
}

func providerRequest(ctx context.Context, client cancellationClient, stream bool) error {
	request := llm.ChatRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "inspect"}},
		MaxTokens: 32,
	}
	if stream {
		_, err := client.Stream(ctx, request, nil, nil)
		return err
	}

	_, err := client.Chat(ctx, request)
	return err
}

func assertCancellationContract(t *testing.T, client cancellationClient, endpoint *blockedResponse) {
	t.Helper()

	for _, mode := range []string{"chat", "stream"} {
		t.Run(mode, func(t *testing.T) {
			stream := mode == "stream"
			before := endpoint.requests.Load()
			cancelled, stop := context.WithCancel(t.Context())
			stop()

			preflight := make(chan error, 1)
			go func() { preflight <- providerRequest(cancelled, client, stream) }()
			var err error
			select {
			case err = <-preflight:
			case <-time.After(5 * time.Second):
				endpoint.unblock()
				<-preflight
				t.Fatal("pre-cancelled request did not stop")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("pre-cancelled request lost cancellation: %v", err)
			}
			if endpoint.requests.Load() != before {
				t.Fatal("pre-cancelled request contacted the server")
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- providerRequest(ctx, client, stream)
			}()

			select {
			case <-endpoint.entered:
			case err = <-done:
				t.Fatalf("request ended before reaching the body: %v", err)
			case <-ctx.Done():
				endpoint.unblock()
				<-done
				t.Fatal("request never reached the response body")
			}
			cancel()

			// Always join before fixture cleanup, including a cancellation regression.
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				endpoint.unblock()
				<-done
				t.Fatal("client did not stop after cancellation")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("body cancellation lost its error identity: %v", err)
			}

			select {
			case <-endpoint.finished:
			case <-time.After(5 * time.Second):
				endpoint.unblock()
				<-endpoint.finished
				t.Fatal("HTTP handler did not stop after cancellation")
			}
			if endpoint.requests.Load() != before+1 {
				t.Fatal("cancelled request was retried")
			}
		})
	}
}
