package httptransport

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxySelectionBySchemeAndBypass(t *testing.T) {
	original := ActiveProxy()
	t.Cleanup(func() { SetProxyConfig(original) })
	SetProxyConfig(ProxyConfig{})
	t.Setenv("HTTP_PROXY", "http://http.example:8080")
	t.Setenv("HTTPS_PROXY", "https://https.example:8443")
	t.Setenv("ALL_PROXY", "socks5://fallback.example:1080")
	t.Setenv("NO_PROXY", ".internal.example,10.0.0.0/8")
	t.Setenv("REQUEST_METHOD", "")
	verify := func(addr, want string) {
		t.Helper()
		target, err := url.Parse(addr)
		if err != nil {
			t.Fatal(err)
		}
		upstream, err := proxySelector()(target)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if upstream != nil {
			got = upstream.String()
		}
		if got != want {
			t.Fatalf("%s: route %q, want %q", addr, got, want)
		}
	}
	verify("http://example.com", "http://http.example:8080")
	verify("https://example.com", "https://https.example:8443")
	verify("https://foo.internal.example", "")
	verify("https://10.2.3.4", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	verify("https://example.com", "socks5://fallback.example:1080")
	t.Setenv("HTTPS_PROXY", "https://https.example:8443")
	SetProxyConfig(ProxyConfig{Type: ProxySOCKS5, Host: "saved.example", Port: "1080"})
	verify("https://example.com", "socks5://saved.example:1080")
	verify("http://localhost:8080", "")
	SetProxyConfig(ProxyConfig{})
	verify("https://example.com", "https://https.example:8443")
}

func TestHTTPProxyTunnelsToValidatedAddress(t *testing.T) {
	original := ActiveProxy()
	t.Cleanup(func() { SetProxyConfig(original) })
	SetProxyConfig(ProxyConfig{})
	t.Setenv("NO_PROXY", "bypass.example")
	t.Setenv("REQUEST_METHOD", "")
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "Basic Zml4dHVyZTpwYXNz" {
			t.Error("CONNECT did not carry proxy credentials")
		}
		requests <- r.Method + " " + r.Host
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, err = fmt.Fprint(rw, "HTTP/1.1 200 Connection Established\r\n\r\n")
		if err != nil {
			t.Error(err)
			return
		}
		if err := rw.Flush(); err != nil {
			t.Error(err)
			return
		}
		forwarded, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		defer forwarded.Body.Close()
		if forwarded.Host != "8.8.8.8" || forwarded.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy tunnel did not preserve origin request headers")
		}
		_, _ = fmt.Fprint(rw, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
		_ = rw.Flush()
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", strings.Replace(server.URL, "://", "://fixture:pass@", 1))
	client := NetworkClient(false)
	resp, err := client.Get("http://8.8.8.8/test")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := <-requests; got != "CONNECT 8.8.8.8:80" {
		t.Fatalf("unexpected proxy request: %s", got)
	}
	if _, err := client.Get("http://127.0.0.1/"); !errors.Is(err, ErrDestinationDenied) {
		t.Fatalf("loopback request: %v", err)
	}
}
