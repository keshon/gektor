package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

// webfetchMaxBytes caps a fetched page: 5MB on the wire, 48KB in context.
const (
	webfetchMaxBytes   = 5 * 1024 * 1024
	webfetchContextMax = 48 * 1024
	webfetchTimeout    = 30 * time.Second
)

// webfetchTransport overrides the HTTP transport when non-nil. It is a
// test seam: unit tests serve canned bodies for public-looking URLs that
// must validate but can never be dialed in a sandbox.
var webfetchTransport http.RoundTripper

// Webfetch fetches a URL and returns its readable text. Unlike check_url
// (status + 512B prefix for reachability), this extracts readable content
// so the model can reason about a page. HTML is stripped to text; other
// content types pass through truncated.
type Webfetch struct{}

func (Webfetch) Name() string         { return "webfetch" }
func (Webfetch) Mode() agent.ToolMode { return agent.Concurrent }
func (Webfetch) Description() string {
	return "Fetch a URL and return its readable text content (HTML is stripped to text). " +
		"Use this to read web pages; use check_url only to test whether a server is up."
}
func (Webfetch) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string"},
			"max_bytes": {"type": "integer", "description": "optional cap on returned text bytes; omit for the default"}
		},
		"required": ["url"]
	}`)
}

func (t Webfetch) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := decodeFetchArgs(args)
	if err != nil {
		return "", err
	}
	return fetch(ctx, in, fetchSpec{
		tool:    "webfetch",
		label:   "WEBFETCH",
		wireCap: webfetchMaxBytes,
		ctxCap:  webfetchContextMax,
		timeout: webfetchTimeout,
		seam:    webfetchTransport,
		extract: extractReadable,
		empty:   "empty after text extraction",
	})
}

func isHTML(ct, body string) bool {
	lc := strings.ToLower(ct)
	if strings.Contains(lc, "html") {
		return true
	}
	if ct == "" {
		l := strings.ToLower(body)
		return strings.Contains(l, "<html") || strings.Contains(l, "<body") || strings.Contains(l, "<p>")
	}
	return false
}

// stripHTML removes tags, scripts, styles and comments with a one-pass
// scanner (stdlib only), then unescapes common entities.
func stripHTML(s string) string {
	var b strings.Builder
	inTag, inSkip := false, false
	skipTag := ""
	i := 0
	for i < len(s) {
		if !inTag && s[i] == '<' {
			// Comment?
			if strings.HasPrefix(s[i:], "<!--") {
				if end := strings.Index(s[i+4:], "-->"); end >= 0 {
					i += 4 + end + 3
					continue
				}
				break
			}
			// Tag name for script/style skipping.
			j := i + 1
			closing := false
			if j < len(s) && s[j] == '/' {
				closing = true
				j++
			}
			k := j
			for k < len(s) && (isTagChar(s[k])) {
				k++
			}
			name := strings.ToLower(s[j:k])
			if !closing && (name == "script" || name == "style") {
				inSkip, skipTag = true, name
			}
			if closing && name == skipTag {
				inSkip = false
				skipTag = ""
			}
			inTag = true
			i++
			continue
		}
		if inTag {
			if s[i] == '>' {
				inTag = false
				if !inSkip {
					b.WriteByte('\n')
				}
			}
			i++
			continue
		}
		if !inSkip {
			b.WriteByte(s[i])
		}
		i++
	}
	return unescapeEntities(b.String())
}

func isTagChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func unescapeEntities(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
		"&nbsp;", " ", "&copy;", "©", "&mdash;", "—", "&ndash;", "–",
	)
	return r.Replace(s)
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}
