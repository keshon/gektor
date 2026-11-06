package httpnode

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/credentials"
	"github.com/keshon/gektor/internal/integrations/httptransport"
	"io"
	"net/http"
	"strings"
	"time"
)

type Resolver func(name, origin string) (header, value, secret string, err error)
type input struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers"`
	Body       json.RawMessage   `json:"body"`
	Credential string            `json:"credential"`
	Timeout    int               `json:"timeout_seconds"`
}

func Definition(resolve Resolver) automation.Node { return definition(resolve, nil) }
func definition(resolve Resolver, transport http.RoundTripper) automation.Node {
	schema := json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1},"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE","HEAD"]},"headers":{"type":"object","properties":{"Accept":{"type":"string"},"Content-Type":{"type":"string"}},"additionalProperties":false},"body":{},"credential":{"type":"string","pattern":"^[a-zA-Z0-9_-]{1,64}$"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":120}},"required":["url"],"additionalProperties":false}`)
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
	return automation.Node{Name: "http_request", Version: 1, Description: "One public HTTP request with explicit method, JSON body and optional origin-bound credential reference. Redirects and automatic retries are disabled. Returns status_code, body and parsed JSON data when available.", InputSchema: schema,
		Validate: func(raw json.RawMessage) error { _, err := parse(raw); return err },
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
				return "", fmt.Errorf("HTTP request: %s", redact(err.Error()))
			}
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
			if err != nil {
				return "", fmt.Errorf("HTTP response read failed: %s", redact(err.Error()))
			}
			if len(body) > 256<<10 {
				return "", fmt.Errorf("HTTP response exceeds 256 KiB")
			}
			safe := redact(string(body))
			result := map[string]any{"status_code": response.StatusCode, "body": safe}
			if json.Valid([]byte(safe)) {
				result["data"] = json.RawMessage(safe)
			}
			out, err := json.Marshal(result)
			if err != nil {
				return "", err
			}
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return string(out), fmt.Errorf("HTTP status %d; request was not retried", response.StatusCode)
			}
			return string(out), nil
		}}
}
