package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/keshon/tars/internal/agent"
)

// webfetch and fetch_raw are the same request with a different cap and a
// different body transform. This is that request. They used to be ~150
// near-identical lines between them, which is how the two drifted: one
// grew a spill notice the other did not, then the other grew a different
// one.

type fetchArgs struct {
	URL      string `json:"url"`
	MaxBytes *int   `json:"max_bytes"`
}

func decodeFetchArgs(args json.RawMessage) (fetchArgs, error) {
	var in fetchArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return in, fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "url", "max_bytes"); err != nil {
		return in, err
	}
	return in, nil
}

// fetchSpec is the whole difference between the two tools.
type fetchSpec struct {
	tool    string // tool name, for the spill file and errors
	label   string // header the model sees
	wireCap int64
	ctxCap  int
	timeout time.Duration
	seam    http.RoundTripper // test transport override
	// extract turns the wire body into the text the model reads. It gets
	// the Content-Type because only webfetch uses it, to decide whether
	// the body is HTML.
	extract func(contentType, text string) string
	// empty is returned when extract yields nothing. Empty means "render
	// the empty result normally", which is what a raw body wants.
	empty string
}

// fetch performs the whole request: validate the URL, GET it under a
// timeout, cap the wire body, extract text, and format the result the
// model reads -- spilling the full text to disk when the context cap bites.
func fetch(ctx context.Context, in fetchArgs, spec fetchSpec) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, spec.timeout)
	defer cancel()
	clean, err := normalizePublicHTTPURL(in.URL)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clean, nil)
	if err != nil {
		return "", fmt.Errorf("bad url: %w", err)
	}
	req.Header.Set("User-Agent", "tars-agent/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/*;q=0.8,*/*;q=0.1")

	resp, err := networkClient(false, spec.seam).Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch failed: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, spec.wireCap+1))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	truncatedWire := int64(len(body)) > spec.wireCap
	if truncatedWire {
		body = body[:spec.wireCap]
	}

	text := spec.extract(resp.Header.Get("Content-Type"), string(body))
	if text == "" && spec.empty != "" {
		return fmt.Sprintf("%s\nurl: %s\nstatus: %s\n(%s)", spec.label, in.URL, resp.Status, spec.empty), nil
	}

	limit := spec.ctxCap
	if in.MaxBytes != nil && *in.MaxBytes > 0 && *in.MaxBytes < limit {
		limit = *in.MaxBytes
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\nurl: %s\nstatus: %s\n", spec.label, in.URL, resp.Status)
	if truncatedWire {
		b.WriteString("(page truncated on the wire)\n")
	}
	b.WriteString("----\n")
	if len(text) > limit {
		b.WriteString(agent.TruncateMiddle(text, limit))
		fmt.Fprintf(&b, "\n...(text truncated at %d bytes of %d total.", limit, len(text))
		if p := Spill(spec.tool, text); p != "" {
			fmt.Fprintf(&b, " Full text: %s)", p)
		} else {
			b.WriteString(")")
		}
	} else {
		b.WriteString(text)
	}
	return b.String(), nil
}

// extractReadable is webfetch's transform: HTML becomes plain text, and
// runs of blank lines collapse so a page does not arrive as mostly
// whitespace.
func extractReadable(contentType, text string) string {
	if isHTML(contentType, text) {
		text = stripHTML(text)
	}
	return strings.TrimSpace(collapseBlankLines(text))
}