package fileops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testService(t *testing.T) Service {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return Service{WS: ws}
}

func TestDownloadPreservesBinaryBytesAndReturnsChecksum(t *testing.T) {
	s := testService(t)
	payload := []byte{0, 255, 13, 10, 128, 1}
	s.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.Header.Get("Accept-Encoding") != "identity" || r.URL.String() != "https://example.com/file" {
			t.Fatal("unexpected request", r)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/octet-stream"}}, Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload)), Request: r}, nil
	})
	result, err := s.Download(t.Context(), DownloadRequest{URL: "https://example.com/file", Path: "data.bin"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.WS.ReadFile("data.bin")
	if err != nil || !bytes.Equal(data, payload) || result.Size != int64(len(payload)) || result.SHA256 != fmt.Sprintf("%x", sha256.Sum256(payload)) || result.Path != "data.bin" {
		t.Fatal(result, data, err)
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestFailedDownloadsLeaveNoOutput(t *testing.T) {
	for _, name := range []string{"oversize-header", "oversize-body", "broken-body", "redirect", "http-error", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			s.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				status, length := 200, int64(-1)
				var body io.Reader = strings.NewReader("12345")
				switch name {
				case "oversize-header":
					length = 5
				case "broken-body":
					body = io.MultiReader(strings.NewReader("12"), failedReader{})
				case "redirect":
					status = 302
				case "http-error":
					status = 500
				case "cancelled":
					body = cancellingReader{cancel}
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://other.example/file"}}, Body: io.NopCloser(body), ContentLength: length, Request: r}, nil
			})
			_, err := s.Download(ctx, DownloadRequest{URL: "https://example.com/file", Path: "data.bin", MaxBytes: 4})
			if err == nil || calls != 1 {
				t.Fatal("failure was accepted or retried", calls, err)
			}
			files, e := os.ReadDir(s.WS.Root())
			if e != nil || len(files) != 0 {
				t.Fatal("partial output survived", files, e)
			}
		})
	}
}

type cancellingReader struct{ cancel context.CancelFunc }

func (r cancellingReader) Read(p []byte) (int, error) { r.cancel(); p[0] = 'x'; return 1, nil }

func TestDownloadRejectsExistingPathsAndPrivateURLs(t *testing.T) {
	s := testService(t)
	s.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("rejected request reached transport")
		return nil, errors.New("unexpected request")
	})
	if err := s.WS.WriteFile("existing", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, req := range []DownloadRequest{
		{URL: "https://example.com/file", Path: "existing"},
		{URL: "http://127.0.0.1/file", Path: "new"},
		{URL: "http://10.0.0.1/file", Path: "new"},
		{URL: "http://metadata.google.internal/file", Path: "new"},
		{URL: "file:///etc/passwd", Path: "new"},
		{URL: "https://user:password@example.com/file", Path: "new"},
		{URL: "https://example.com/file", Path: "../outside"},
	} {
		if _, err := s.Download(t.Context(), req); err == nil {
			t.Fatal("accepted", req)
		}
	}
	data, _ := s.WS.ReadFile("existing")
	if string(data) != "keep" {
		t.Fatal("overwrote existing file")
	}
}

func TestDownloadCannotFollowWorkspaceEscapeLink(t *testing.T) {
	s := testService(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.WS.Root(), "link")); err != nil {
		t.Skip(err)
	}
	s.transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("escape reached network"); return nil, nil })
	if _, err := s.Download(t.Context(), DownloadRequest{URL: "https://example.com/file", Path: "link/file"}); err == nil {
		t.Fatal("followed escaping symlink")
	}
	files, err := os.ReadDir(outside)
	if err != nil || len(files) != 0 {
		t.Fatal("wrote outside workspace", files, err)
	}
}
