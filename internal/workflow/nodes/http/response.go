package httpnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/workflow"
)

type redactor struct{ patterns []string }

func (r *redactor) add(secret string) error {
	if secret == "" {
		return nil
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		return err
	}

	escapedJSON := string(encoded[1 : len(encoded)-1])
	escapedQuery := url.QueryEscape(secret)
	escapedPath := url.PathEscape(secret)
	for _, pattern := range []string{escapedJSON, secret, escapedQuery, escapedPath} {
		if !slices.Contains(r.patterns, pattern) {
			r.patterns = append(r.patterns, pattern)
		}
	}
	return nil
}

func (r redactor) text(value string) string {
	for _, pattern := range r.patterns {
		value = strings.ReplaceAll(value, pattern, "[redacted]")
	}
	return value
}

type responseResult struct {
	Status   int                `json:"status_code"`
	Headers  map[string]string  `json:"headers"`
	Body     string             `json:"body"`
	Data     json.RawMessage    `json:"data,omitempty"`
	Artifact *workflow.Artifact `json:"artifact,omitempty"`
}

func consumeResponse(ctx context.Context, response *http.Response, in input, secrets redactor, access workflow.ArtifactAccess) (string, error) {
	defer response.Body.Close() // HTTP owns the body; artifact access only borrows its reader.
	result := responseResult{Status: response.StatusCode, Headers: map[string]string{}}
	names := []string{"Content-Type", "Retry-After", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"}
	names = append(names, in.ResponseHeaders...)
	total := 0
	for _, name := range names {
		value := response.Header.Get(name)
		if value == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := result.Headers[key]; exists {
			continue
		}
		total += len(value)
		if len(value) > maxHeaderValueBytes || total > maxHeaderTotalBytes {
			return "", &workflow.ExecutionError{Code: "http_response_too_large", Err: fmt.Errorf("HTTP response headers exceed their limit")}
		}
		result.Headers[key] = secrets.text(value)
	}

	if in.ResponseFormat == responseArtifact {
		// Store failures stay terminal. Put discards incomplete/oversized/secret-bearing responses.
		body := &artifactBody{source: response.Body, remaining: in.MaxResponseBytes, patterns: secrets.patterns}
		for _, pattern := range secrets.patterns {
			body.keep = max(body.keep, len(pattern)-1)
		}
		meta, err := access.Put(ctx, in.ArtifactName, body)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if errors.Is(err, context.Canceled) {
				return "", context.Canceled
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return "", context.DeadlineExceeded
			}
			return "", fmt.Errorf("saving HTTP response artifact: %s", secrets.text(err.Error()))
		}
		result.Artifact = &meta
	} else {
		body, err := io.ReadAll(io.LimitReader(response.Body, in.MaxResponseBytes+1))
		if err != nil {
			return "", requestFailure(ctx, err, secrets, "http_response_failed")
		}
		if int64(len(body)) > in.MaxResponseBytes {
			return "", &workflow.ExecutionError{Code: "http_response_too_large", Err: fmt.Errorf("HTTP response exceeds configured limit")}
		}
		if !utf8.Valid(body) {
			return "", &workflow.ExecutionError{Code: "http_response_encoding", Err: fmt.Errorf("HTTP response is not UTF-8; use artifact mode")}
		}
		result.Body = secrets.text(string(body))
		validJSON := json.Valid([]byte(result.Body))
		if in.ResponseFormat == responseJSON && !validJSON {
			return "", &workflow.ExecutionError{Code: "http_response_invalid_json", Err: fmt.Errorf("HTTP response is not JSON")}
		}
		if validJSON && in.ResponseFormat != responseText {
			result.Data = json.RawMessage(result.Body)
		}
	}
	out, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	if len(out) > maxOutputBytes {
		return "", &workflow.ExecutionError{Code: "http_response_too_large", Err: fmt.Errorf("encoded HTTP output exceeds 1 MiB; use artifact mode")}
	}
	if !in.StatusAsData && (response.StatusCode < 200 || response.StatusCode >= 300) {
		return string(out), &workflow.ExecutionError{Code: "http_status", Err: fmt.Errorf("HTTP status %d; request was not retried", response.StatusCode)}
	}
	return string(out), nil
}

// artifactBody bounds the stream and withholds a suffix so credentials spanning
// read boundaries are rejected before they can be published as an artifact.
type artifactBody struct {
	source    io.Reader
	remaining int64
	patterns  []string
	keep      int
	pending   []byte
	finished  bool
}

func (b *artifactBody) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return 0, nil
	}
	for !b.finished && len(b.pending) <= b.keep {
		var chunk [32 << 10]byte
		capacity := int64(len(chunk))
		if capacity > b.remaining+1 {
			capacity = b.remaining + 1
		}
		n, err := b.source.Read(chunk[:capacity])
		if int64(n) > b.remaining {
			return 0, fmt.Errorf("response exceeds configured artifact limit")
		}
		b.remaining -= int64(n)
		b.pending = append(b.pending, chunk[:n]...)
		for _, pattern := range b.patterns {
			if bytes.Contains(b.pending, []byte(pattern)) {
				return 0, fmt.Errorf("response contains a source credential")
			}
		}
		if err != nil && err != io.EOF {
			return 0, err
		}
		b.finished = err == io.EOF
		if n == 0 && err == nil {
			return 0, nil
		}
	}
	available := len(b.pending)
	if !b.finished {
		available -= b.keep
	}
	if available == 0 && b.finished {
		return 0, io.EOF
	}
	n := copy(destination, b.pending[:available])
	b.pending = b.pending[n:]
	return n, nil
}
