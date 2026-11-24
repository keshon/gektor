package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ReadThread   = "read_thread"
	ReadTimeline = "read_timeline"
	ReadTrends   = "read_trends"
)

// PageRequest selects a single provider page. Limit requests a timeline/trend page size; providers may return more posts.
type PageRequest struct {
	URL    string `json:"url,omitempty"`
	Handle string `json:"handle,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type Trend struct {
	Name    string `json:"name"`
	Rank    string `json:"rank,omitempty"`
	Context string `json:"context,omitempty"`
}

// Page distinguishes provider pagination from locally omitted content.
type Page struct {
	Status      string  `json:"status"`
	Post        *Post   `json:"post,omitempty"`
	Posts       []Post  `json:"posts,omitempty"`
	Thread      []Post  `json:"thread,omitempty"`
	Replies     []Post  `json:"replies,omitempty"`
	Trends      []Trend `json:"trends,omitempty"`
	NextCursor  string  `json:"next_cursor,omitempty"`
	More        bool    `json:"more"`
	Truncated   bool    `json:"truncated"`
	Source      string  `json:"source"`
	RetrievedAt string  `json:"retrieved_at"`
}

func ValidatePage(kind string, in PageRequest) (PageRequest, string, error) {
	if kind == ReadThread {
		if in.Limit != 0 {
			return in, "", fmt.Errorf("thread reads do not accept a limit; the provider controls reply page size")
		}
	} else {
		if in.Limit == 0 {
			in.Limit = 10
		}
		if in.Limit < 1 || in.Limit > 20 {
			return in, "", fmt.Errorf("limit must be between 1 and 20")
		}
	}
	if len(in.Cursor) > 4096 || strings.ContainsAny(in.Cursor, "\r\n") {
		return in, "", fmt.Errorf("invalid pagination cursor")
	}
	switch kind {
	case ReadThread:
		if len(in.URL) > 2048 {
			return in, "", fmt.Errorf("post URL exceeds 2048 bytes")
		}
		id, err := postID(in.URL)
		if err != nil {
			return in, "", err
		}
		in.URL = "https://x.com/i/status/" + id
		return in, in.URL, nil
	case ReadTimeline:
		in.Handle = strings.TrimPrefix(strings.TrimSpace(in.Handle), "@")
		if !handlePattern.MatchString(in.Handle) {
			return in, "", fmt.Errorf("expected an X/Twitter handle of 1–15 letters, numbers or underscores")
		}
		in.Handle = strings.ToLower(in.Handle)
		return in, "https://x.com/" + in.Handle, nil
	case ReadTrends:
		if in.Cursor != "" {
			return in, "", fmt.Errorf("trends do not support pagination")
		}
		return in, "https://x.com/explore", nil
	default:
		return in, "", fmt.Errorf("unsupported social operation %q", kind)
	}
}

func (s Service) Query(ctx context.Context, kind string, in PageRequest) (Page, error) {
	page := Page{Status: "invalid_response", Source: "FxTwitter", RetrievedAt: time.Now().UTC().Format(time.RFC3339)}
	in, _, err := ValidatePage(kind, in)
	if err != nil {
		return page, err
	}
	if s.Client == nil {
		return page, fmt.Errorf("social HTTP client unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	path := "/2/trends"
	query := url.Values{}
	switch kind {
	case ReadThread:
		id, err := postID(in.URL)
		if err != nil {
			return page, err
		}
		path = "/2/conversation/" + id
		query.Set("ranking_mode", "recency")
	case ReadTimeline:
		path = "/2/profile/" + in.Handle + "/statuses"
		query.Set("count", strconv.Itoa(in.Limit))
	case ReadTrends:
		query.Set("count", strconv.Itoa(in.Limit))
	}
	if in.Cursor != "" {
		query.Set("cursor", in.Cursor)
	}
	base := s.Endpoint
	if base == "" {
		base = socialEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+query.Encode(), nil)
	if err != nil {
		return page, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gektor-social-reader/1.0 (+https://github.com/keshon/gektor)")
	// Copy the client so an injected client cannot follow provider redirects to arbitrary hosts.
	client := *s.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		page.Status = "transient_error"
		return page, fmt.Errorf("read social page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		page.Status = accessStatus(resp.StatusCode)
		return page, fmt.Errorf("%s: social page unavailable; do not treat as empty results", page.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		page.Status = "transient_error"
		return page, fmt.Errorf("read social response: %w", err)
	}
	if len(body) > 2<<20 {
		return page, fmt.Errorf("social response exceeds 2 MiB")
	}
	var result providerPage
	err = json.Unmarshal(body, &result)
	if err != nil {
		return page, fmt.Errorf("decode social response: %w", err)
	}
	if result.Code != http.StatusOK {
		page.Status = accessStatus(result.Code)
		return page, fmt.Errorf("%s: provider could not retrieve social page", page.Status)
	}
	if (kind == ReadThread && result.Status == nil) || (kind == ReadTimeline && result.Results == nil) || (kind == ReadTrends && result.Trends == nil) {
		return page, fmt.Errorf("invalid_response: missing social page data")
	}
	err = ctx.Err()
	if err != nil {
		return page, err
	}
	page.populate(result, in, kind)
	err = page.bound()
	if err != nil {
		return page, err
	}

	if page.Truncated {
		page.Status = "truncated"
	}
	if page.Status != "ok" {
		return page, fmt.Errorf("%s: social page incomplete; report the limitation", page.Status)
	}
	return page, nil
}
