package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Listen deliberately accepts only literal loopback addresses. Remote ingress
// belongs behind a TLS reverse proxy; the listener itself never handles TLS keys.
func Listen(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("workflow API listen address must use a literal loopback IP")
	}
	return net.Listen("tcp", address)
}

// Serve joins both owners before returning so the caller can safely close its
// workspace/store. Cancellation reaches active jobs through the worker context.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler, worker func(context.Context) error) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var requests sync.WaitGroup
	var admission sync.Mutex
	closing := false
	tracked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admission.Lock()
		if closing {
			admission.Unlock()
			http.Error(w, "service stopping", http.StatusServiceUnavailable)
			return
		}
		requests.Add(1)
		admission.Unlock()
		defer requests.Done()
		handler.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: tracked, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan error, 2)
	go func() { done <- worker(ctx) }()
	go func() { done <- server.Serve(listener) }()
	var result error
	collect := func(err error) {
		if !normalShutdown(err) {
			result = errors.Join(result, err)
		}
	}
	remaining := 2
	select {
	case err := <-done:
		collect(err)
		remaining--
	case <-ctx.Done():
	}
	stop()
	admission.Lock()
	closing = true
	admission.Unlock()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownErr := server.Shutdown(shutdownCtx)
	cancel()
	if shutdownErr != nil {
		_ = server.Close()
	}
	for ; remaining > 0; remaining-- {
		err := <-done
		collect(err)
	}
	// Close only closes sockets. A handler may still be completing validation or
	// persistence; keep its workspace and store alive until it actually returns.
	requests.Wait()
	return errors.Join(result, shutdownErr)
}

// A cancellation joined with a persistence failure is still a failure.
func normalShutdown(err error) bool {
	if err == nil || err == context.Canceled || err == http.ErrServerClosed {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !normalShutdown(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return normalShutdown(wrapped.Unwrap())
	}
	return false
}
