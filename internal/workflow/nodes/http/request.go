package httpnode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/keshon/gektor/internal/httptransport"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/credentials"
)

type bodyFormat string
type responseFormat string

const (
	bodyJSON         bodyFormat     = "json"
	bodyText         bodyFormat     = "text"
	bodyForm         bodyFormat     = "form"
	bodyArtifact     bodyFormat     = "artifact"
	responseAuto     responseFormat = "auto"
	responseText     responseFormat = "text"
	responseJSON     responseFormat = "json"
	responseArtifact responseFormat = "artifact"
)

var headerNames = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)

type input struct {
	URL                string              `json:"url"`
	Method             string              `json:"method,omitempty"`
	Query              map[string][]string `json:"query,omitempty"`
	Headers            map[string]string   `json:"headers,omitempty"`
	Body               json.RawMessage     `json:"body,omitempty"`
	BodyFormat         bodyFormat          `json:"body_format,omitempty"`
	BodyArtifact       string              `json:"body_artifact,omitempty"`
	Credential         string              `json:"credential,omitempty"`
	CredentialRevision string              `json:"credential_revision,omitempty"`
	Timeout            int                 `json:"timeout_seconds,omitempty"`
	StatusAsData       bool                `json:"status_as_data,omitempty"`
	ResponseFormat     responseFormat      `json:"response_format,omitempty"`
	ResponseHeaders    []string            `json:"response_headers,omitempty"`
	MaxResponseBytes   int64               `json:"max_response_bytes,omitempty"`
	ArtifactName       string              `json:"artifact_name,omitempty"`
	MaxRedirects       int                 `json:"max_redirects,omitempty"`
}

const inputSchema = `{
 "type":"object",
 "properties":{
  "url":{"type":"string","minLength":1,"maxLength":32768},
  "method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE","HEAD"]},
  "query":{"type":"object","maxProperties":64,"additionalProperties":{"type":"array","maxItems":64,"items":{"type":"string"}}},
  "headers":{"type":"object","properties":{"Accept":{"type":"string"},"Content-Type":{"type":"string"}},"additionalProperties":false},
  "body":{},
  "body_format":{"type":"string","enum":["json","text","form","artifact"]},
  "body_artifact":{"type":"string","minLength":1,"maxLength":64},
  "credential":{"type":"string","pattern":"^[a-zA-Z0-9_-]{1,64}$"},
  "credential_revision":{"type":"string","pattern":"^[a-f0-9]{64}$"},
  "timeout_seconds":{"type":"integer","minimum":1,"maximum":120},
  "status_as_data":{"type":"boolean"},
  "response_format":{"type":"string","enum":["auto","text","json","artifact"]},
  "response_headers":{"type":"array","maxItems":16,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":128}},
  "max_response_bytes":{"type":"integer","minimum":1,"maximum":16777216},
  "artifact_name":{"type":"string","minLength":1,"maxLength":255},
  "max_redirects":{"type":"integer","minimum":0,"maximum":5}
 },
 "required":["url"],
 "allOf":[
  {"if":{"properties":{"body_format":{"const":"artifact"}},"required":["body_format"]},"then":{"required":["body_artifact"],"not":{"required":["body"]}},"else":{"not":{"required":["body_artifact"]}}},
  {"if":{"properties":{"body_format":{"const":"text"}},"required":["body_format"]},"then":{"properties":{"body":{"type":"string"}}}},
  {"if":{"properties":{"body_format":{"const":"form"}},"required":["body_format"]},"then":{"properties":{"body":{"type":"object","additionalProperties":{"type":"array","items":{"type":"string"}}}}}},
  {"if":{"properties":{"response_format":{"const":"artifact"}},"required":["response_format"]},"then":{"required":["artifact_name"]},"else":{"not":{"required":["artifact_name"]}}}
 ],
 "dependentRequired":{"credential":["credential_revision"],"credential_revision":["credential"]},
 "additionalProperties":false
}`

func parse(raw json.RawMessage) (input, error) {
	var in input
	err := workflow.ValidateJSON(raw, json.RawMessage(inputSchema))
	if err != nil {
		return in, err
	}
	err = json.Unmarshal(raw, &in)
	if err != nil {
		return in, err
	}

	if in.Method == "" {
		in.Method = http.MethodGet
	}
	if in.BodyFormat == "" {
		in.BodyFormat = bodyJSON
	}
	if in.ResponseFormat == "" {
		in.ResponseFormat = responseAuto
	}
	if in.Timeout == 0 {
		in.Timeout = 30
	}
	if in.Credential != "" {
		_, err = credentials.Origin(in.URL)
		if err != nil {
			return in, err
		}
	}

	normalized, err := httptransport.Normalize(in.URL, false)
	if err != nil {
		return in, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return in, err
	}
	query := parsed.Query()
	for name, values := range in.Query {
		query.Del(name)
		for _, value := range values {
			query.Add(name, value)
		}
	}
	parsed.RawQuery = query.Encode()
	in.URL, err = httptransport.Normalize(parsed.String(), false)
	if err != nil {
		return in, err
	}
	if len(in.URL) > maxURLBytes {
		return in, fmt.Errorf("HTTP URL exceeds 32 KiB")
	}
	in.Query = nil // The normalized URL owns the merged query, including during redirects.
	if in.BodyFormat != bodyArtifact {
		_, _, err = inlineBody(in)
		if err != nil {
			return in, err
		}
	}
	if in.ResponseFormat == responseArtifact {
		invalidName := len(in.ArtifactName) > 255 || in.ArtifactName == "." || in.ArtifactName == ".." || strings.ContainsAny(in.ArtifactName, "/\\\x00\r\n")
		if invalidName {
			return in, fmt.Errorf("artifact response requires a filename without path separators")
		}
	}
	limit := int64(inlineLimit)
	if in.ResponseFormat == responseArtifact {
		limit = artifactLimit
	}
	if in.MaxResponseBytes == 0 {
		in.MaxResponseBytes = limit
	}
	if in.MaxResponseBytes > limit {
		return in, fmt.Errorf("inline HTTP responses cannot exceed 256 KiB; use artifact mode")
	}
	for _, name := range in.ResponseHeaders {
		sensitive := strings.ToLower(name)
		if !headerNames.MatchString(name) || sensitive == "set-cookie" || sensitive == "cookie" || sensitive == "authorization" || sensitive == "proxy-authorization" {
			return in, fmt.Errorf("response header is invalid or credential-bearing")
		}
	}
	return in, nil
}

func inlineBody(in input) ([]byte, string, error) {
	if len(in.Body) > inlineBodyLimit {
		return nil, "", fmt.Errorf("inline HTTP body exceeds 64 KiB; use an artifact")
	}

	switch in.BodyFormat {
	case bodyJSON:
		return in.Body, "application/json", nil
	case bodyText:
		if len(in.Body) == 0 {
			return nil, "text/plain; charset=utf-8", nil
		}
		var text string
		err := json.Unmarshal(in.Body, &text)
		return []byte(text), "text/plain; charset=utf-8", err
	case bodyForm:
		var fields map[string][]string
		if len(in.Body) > 0 {
			err := json.Unmarshal(in.Body, &fields)
			if err != nil {
				return nil, "", fmt.Errorf("form body requires an object of string arrays")
			}
		}

		values := url.Values(fields)
		encoded := values.Encode()
		if len(encoded) > inlineBodyLimit {
			return nil, "", fmt.Errorf("encoded form body exceeds 64 KiB; use an artifact")
		}
		return []byte(encoded), "application/x-www-form-urlencoded", nil
	}
	return nil, "", fmt.Errorf("unknown HTTP body format")
}

func requestBody(ctx context.Context, in input, access workflow.ArtifactAccess) ([]byte, string, error) {
	if in.BodyFormat != bodyArtifact {
		return inlineBody(in)
	}
	meta, body, err := access.Read(ctx, in.BodyArtifact)
	if err != nil {
		return nil, "", err
	}
	return body, meta.MediaType, nil
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

func redirectInput(response *http.Response, in input) (input, error) {
	if len(response.Header.Get("Location")) > maxURLBytes {
		return in, fmt.Errorf("redirect URL exceeds 32 KiB")
	}
	target, err := response.Location()
	if err != nil {
		return in, err
	}
	next, err := httptransport.Normalize(target.String(), false)
	if err != nil {
		return in, err
	}
	if in.Credential != "" {
		before, originErr := credentials.Origin(in.URL)
		if originErr != nil {
			return in, originErr
		}
		after, originErr := credentials.Origin(next)
		if originErr != nil || before != after {
			return in, fmt.Errorf("credentialed redirects must stay on the same HTTPS origin")
		}
	}
	seeOtherToGet := response.StatusCode == http.StatusSeeOther && in.Method != http.MethodHead
	postToGet := in.Method == http.MethodPost && (response.StatusCode == http.StatusMovedPermanently || response.StatusCode == http.StatusFound)
	if seeOtherToGet || postToGet {
		in.Method = http.MethodGet
		in.Body = nil
		in.BodyArtifact = ""
		in.BodyFormat = bodyJSON
		if in.Headers != nil {
			headers := make(map[string]string, len(in.Headers))
			for name, value := range in.Headers {
				if !strings.EqualFold(name, "Content-Type") {
					headers[name] = value
				}
			}
			in.Headers = headers
		}
	}
	in.URL = next
	return in, nil
}
