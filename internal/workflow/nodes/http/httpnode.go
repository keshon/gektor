// Package httpnode adapts bounded HTTP requests to workflow nodes.
package httpnode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/httptransport"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/credentials"
)

type Resolver func(name, origin string) (header, value, secret string, err error)
type input struct {
	URL          string            `json:"url"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers"`
	Body         json.RawMessage   `json:"body"`
	Credential   string            `json:"credential"`
	Timeout      int               `json:"timeout_seconds"`
	StatusAsData bool              `json:"status_as_data"`
}

func Definition(resolve Resolver) workflow.Node { return definition(resolve, nil) }
func definition(resolve Resolver, transport http.RoundTripper) workflow.Node {
	schema := json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1},"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE","HEAD"]},"headers":{"type":"object","properties":{"Accept":{"type":"string"},"Content-Type":{"type":"string"}},"additionalProperties":false},"body":{},"credential":{"type":"string","pattern":"^[a-zA-Z0-9_-]{1,64}$"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":120},"status_as_data":{"type":"boolean"}},"required":["url"],"additionalProperties":false}`)
	parse := func(raw json.RawMessage) (input, error) {
		var in input
		if err := json.Unmarshal(raw, &in); err != nil {
			return in, err
		}

		if in.Method == "" {
			in.Method = "GET"
		}

		var err error
		in.URL, err = httptransport.Normalize(in.URL, false)
		if err != nil {
			return in, err
		}

		if in.Credential != "" {
			_, err = credentials.Origin(in.URL)
		}

		return in, err
	}
	return workflow.Node{
		Name:         "http_request",
		Concurrent:   true,
		Version:      1,
		Description:  "One public HTTP request with explicit method, JSON body and optional origin-bound credential reference. No redirects or automatic retries. Returns status_code, selected response headers, body and parsed JSON data. status_as_data returns non-2xx responses without failing; network and response-read errors still fail.",
		InputSchema:  schema,
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"status_code":{"type":"integer"},"headers":{"type":"object","additionalProperties":{"type":"string"}},"body":{"type":"string"},"data":{}},"required":["status_code","headers","body"]}`),
		Validate:     func(raw json.RawMessage) error { _, err := parse(raw); return err },
		Resources: func(raw json.RawMessage) ([]string, error) {
			in, err := parse(raw)
			if err != nil {
				return nil, err
			}

			resources := []string{in.Method + " " + in.URL}
			if in.Credential != "" {
				origin, _ := credentials.Origin(in.URL)
				resources = append(resources, "credential:"+in.Credential+"@"+origin)
			}

			return resources, nil
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			in, err := parse(raw)
			if err != nil {
				return "", err
			}

			if in.Timeout == 0 {
				in.Timeout = 30
			}

			ctx, cancel := context.WithTimeout(ctx, time.Duration(in.Timeout)*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, in.Method, in.URL, strings.NewReader(string(in.Body)))
			if err != nil {
				return "", err
			}

			if len(in.Body) > 0 {
				req.Header.Set("Content-Type", "application/json")
			}

			for k, v := range in.Headers {
				req.Header.Set(k, v)
			}

			secret := ""
			if in.Credential != "" {
				if resolve == nil {
					return "", fmt.Errorf("credential resolver unavailable")
				}

				header, value, s, e := resolve(in.Credential, in.URL)
				if e != nil {
					return "", e
				}

				secret = s
				req.Header.Set(header, value)
			}

			redact := func(value string) string {
				if secret != "" {
					return strings.ReplaceAll(value, secret, "[redacted]")
				}

				return value
			}
			client := httptransport.Client(false, transport)
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			response, err := client.Do(req)
			if err != nil {
				return "", &workflow.ExecutionError{Err: fmt.Errorf("HTTP request: %s", redact(err.Error()))}
			}

			defer response.Body.Close()

			body, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
			if err != nil {
				return "", &workflow.ExecutionError{Err: fmt.Errorf("HTTP response read failed: %s", redact(err.Error()))}
			}

			if len(body) > 256<<10 {
				return "", &workflow.ExecutionError{Err: fmt.Errorf("HTTP response exceeds 256 KiB")}
			}

			safe := redact(string(body))
			headers := map[string]string{}

			for _, name := range []string{"Content-Type", "Retry-After", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"} {
				if value := response.Header.Get(name); value != "" {
					if len(value) > 4096 {
						return "", fmt.Errorf("HTTP response header %s exceeds 4 KiB", name)
					}

					headers[strings.ToLower(name)] = redact(value)
				}
			}

			result := map[string]any{
				"status_code": response.StatusCode,
				"headers":     headers,
				"body":        safe,
			}
			if json.Valid([]byte(safe)) {
				result["data"] = json.RawMessage(safe)
			}

			out, err := json.Marshal(result)
			if err != nil {
				return "", err
			}

			if !in.StatusAsData && (response.StatusCode < 200 || response.StatusCode >= 300) {
				return string(out), &workflow.ExecutionError{Err: fmt.Errorf("HTTP status %d; request was not retried", response.StatusCode)}
			}

			return string(out), nil
		},
	}
}
