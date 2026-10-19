package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/keshon/tars/internal/agent"
)

// fetchRawMaxBytes caps a fetched page: 5MB on the wire, 128KB in context.
const (
	fetchRawMaxBytes   = 5 * 1024 * 1024
	fetchRawContextMax = 128 * 1024
	fetchRawTimeout    = 30 * time.Second
)

// fetchRawTransport overrides the HTTP transport when non-nil. It is a
// test seam: unit tests serve canned bodies for public-looking URLs that
// must validate but can never be dialed in a sandbox.
var fetchRawTransport http.RoundTripper

// FetchRaw fetches a URL and returns its raw content. Unlike webfetch, it
// does not strip HTML, making it suitable for parsing page structure.
type FetchRaw struct{}

func (FetchRaw) Name() string         { return "fetch_raw" }
func (FetchRaw) Mode() agent.ToolMode { return agent.Concurrent }
func (FetchRaw) Description() string {
	return "Fetch a URL and return its raw HTML/content. Use this when you need to parse the structure of a page (e.g., for searching)."
}
func (FetchRaw) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string"},
			"max_bytes": {"type": "integer", "description": "optional cap on returned text bytes; omit for the default"}
		},
		"required": ["url"]
	}`)
}

func (t FetchRaw) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := decodeFetchArgs(args)
	if err != nil {
		return "", err
	}
	return fetch(ctx, in, fetchSpec{
		tool:    "fetch_raw",
		label:   "FETCH_RAW",
		wireCap: fetchRawMaxBytes,
		ctxCap:  fetchRawContextMax,
		timeout: fetchRawTimeout,
		seam:    fetchRawTransport,
		extract: func(_, text string) string { return text },
	})
}
