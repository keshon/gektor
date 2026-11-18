// Package httpnode adapts bounded HTTP requests to workflow nodes.
package httpnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/keshon/gektor/internal/httptransport"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/credentials"
)

type Resolver func(name, origin, revision string) (header, value, secret string, err error)

const (
	inlineLimit         = 256 << 10
	inlineBodyLimit     = 64 << 10
	artifactLimit       = 16 << 20
	maxURLBytes         = 32 << 10
	maxHeaderValueBytes = 4 << 10
	maxHeaderTotalBytes = 32 << 10
	maxOutputBytes      = 1 << 20
)

func Definition(resolve Resolver) workflow.Node { return definition(resolve, nil) }

func definition(resolve Resolver, transport http.RoundTripper) workflow.Node {
	return workflow.Node{
		Name:         "http_request",
		Version:      1,
		Concurrent:   true,
		Description:  "Public HTTP request with explicit query/body formats and auto, text, JSON or run-artifact responses. Optional pinned origin-bound credential. Redirects are off by default; max_redirects permits up to 5 separately authorized destinations. Credentials never cross origins. No automatic retries; status_as_data returns non-2xx status without hiding transport, body or policy errors.",
		InputSchema:  json.RawMessage(inputSchema),
		OutputSchema: json.RawMessage(outputSchema),
		Validate:     func(raw json.RawMessage) error { _, err := parse(raw); return err },
		Resources:    resources,
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			in, err := parse(raw)
			if err != nil {
				return "", err
			}
			return execute(ctx, in, resolve, transport)
		},
	}
}

func resources(raw json.RawMessage) ([]string, error) {
	in, err := parse(raw)
	if err != nil {
		return nil, err
	}
	targets := []string{in.Method + " " + in.URL}
	if in.Credential != "" {
		origin, originErr := credentials.Origin(in.URL)
		if originErr != nil {
			return nil, originErr
		}
		targets = append(targets, "credential:"+in.Credential+"@"+origin+"#"+in.CredentialRevision)
	}
	if in.BodyArtifact != "" {
		targets = append(targets, in.BodyArtifact)
	}
	if in.ResponseFormat == responseArtifact {
		targets = append(targets, in.ArtifactName)
	}
	return targets, nil
}

func execute(ctx context.Context, in input, resolve Resolver, transport http.RoundTripper) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(in.Timeout)*time.Second)
	defer cancel()

	var access workflow.ArtifactAccess
	var err error
	if in.BodyFormat == bodyArtifact || in.ResponseFormat == responseArtifact {
		access, err = workflow.Artifacts(requestCtx)
		if err != nil {
			return "", err
		}
	}

	payload, contentType, err := requestBody(requestCtx, in, access)
	if err != nil {
		return "", err
	}

	client := httptransport.Client(false, transport)
	client.Timeout = 0 // The request context covers the entire redirect chain.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var secrets redactor

	for redirects := 0; ; redirects++ {
		req, requestErr := http.NewRequestWithContext(requestCtx, in.Method, in.URL, bytes.NewReader(payload))
		if requestErr != nil {
			return "", requestErr
		}
		if len(in.Body) > 0 || in.BodyArtifact != "" || in.BodyFormat != bodyJSON {
			req.Header.Set("Content-Type", contentType)
		}
		for name, value := range in.Headers {
			req.Header.Set(name, value)
		}
		if in.Credential != "" {
			if resolve == nil {
				return "", fmt.Errorf("credential resolver unavailable")
			}
			header, value, secret, resolveErr := resolve(in.Credential, in.URL, in.CredentialRevision)
			if resolveErr != nil {
				return "", resolveErr
			}
			err = secrets.add(secret)
			if err != nil {
				return "", err
			}
			req.Header.Set(header, value)
		}

		response, requestErr := client.Do(req)
		if requestErr != nil {
			return "", requestFailure(requestCtx, requestErr, secrets, "http_request_failed")
		}
		redirect := isRedirect(response.StatusCode) && response.Header.Get("Location") != "" && in.MaxRedirects > 0
		if !redirect {
			return consumeResponse(requestCtx, response, in, secrets, access)
		}
		// Intermediate bodies are never published; Close releases the connection.
		response.Body.Close()
		if redirects >= in.MaxRedirects {
			return "", &workflow.ExecutionError{Code: "http_redirect_limit", Err: fmt.Errorf("HTTP redirect limit reached")}
		}
		next, redirectErr := redirectInput(response, in)
		if redirectErr != nil {
			return "", fmt.Errorf("%w: %s", httptransport.ErrDestinationDenied, secrets.text(redirectErr.Error()))
		}
		if secrets.text(next.URL) != next.URL {
			return "", fmt.Errorf("%w: redirect URL contains a source credential", httptransport.ErrDestinationDenied)
		}
		raw, encodeErr := json.Marshal(next)
		if encodeErr != nil {
			return "", encodeErr
		}
		err = workflow.AuthorizeCall(requestCtx, "http_request", raw)
		if err != nil {
			if requestCtx.Err() != nil {
				return "", requestCtx.Err()
			}
			if errors.Is(err, context.Canceled) {
				return "", context.Canceled
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return "", context.DeadlineExceeded
			}
			return "", fmt.Errorf("HTTP redirect authorization: %s", secrets.text(err.Error()))
		}
		if next.Method != in.Method {
			payload = nil
			contentType = ""
		}
		in = next
	}
}

func requestFailure(ctx context.Context, err error, secrets redactor, code string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, httptransport.ErrDestinationDenied) {
		return fmt.Errorf("%w: %s", httptransport.ErrDestinationDenied, secrets.text(err.Error()))
	}
	action := "sending HTTP request"
	if code == "http_response_failed" {
		action = "reading HTTP response"
	}
	return &workflow.ExecutionError{Code: code, Err: fmt.Errorf("%s: %s", action, secrets.text(err.Error()))}
}

const outputSchema = `{
 "type":"object",
 "properties":{
  "status_code":{"type":"integer"},
  "headers":{"type":"object","additionalProperties":{"type":"string"}},
  "body":{"type":"string"},
  "data":{},
  "artifact":{
   "type":"object",
   "properties":{
    "id":{"type":"string"},"run_id":{"type":"string"},"step_id":{"type":"string"},
    "name":{"type":"string"},"media_type":{"type":"string"},"size":{"type":"integer"},
    "sha256":{"type":"string"},"created":{"type":"string"}
   },
   "required":["id","run_id","step_id","name","media_type","size","sha256","created"]
  }
 },
 "required":["status_code","headers","body"]
}`
