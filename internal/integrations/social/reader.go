// Package social reads public posts through the FxTwitter provider. Callers
// supply the HTTP client and own authorization and timeouts.
package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const socialEndpoint = "https://api.fxtwitter.com"

var postPath = regexp.MustCompile(`^/(?:[A-Za-z0-9_]{1,15}/status|i/web/status)/([0-9]{1,25})/?$`)

// Post records retrieved content and explicit access or truncation status.
type Post struct {
	Status      string   `json:"status"`
	URL         string   `json:"url,omitempty"`
	Author      string   `json:"author,omitempty"`
	Handle      string   `json:"handle,omitempty"`
	Text        string   `json:"text,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	Media       []string `json:"media,omitempty"`
	RetrievedAt string   `json:"retrieved_at"`
	Source      string   `json:"source"`
	Truncated   bool     `json:"truncated"`
}

func postID(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", fmt.Errorf("expected an HTTPS X/Twitter post URL")
	}
	switch strings.ToLower(u.Hostname()) {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com", "mobile.twitter.com":
	default:
		return "", fmt.Errorf("unsupported post host")
	}
	parts := postPath.FindStringSubmatch(u.Path)
	if len(parts) != 2 {
		return "", fmt.Errorf("expected a single post URL ending in /status/<id>")
	}
	return parts[1], nil
}

// ReadPost accepts a public post URL. An empty base selects FxTwitter.
func ReadPost(ctx context.Context, client *http.Client, base, raw string) Post {
	p := Post{Status: "unsupported", RetrievedAt: time.Now().UTC().Format(time.RFC3339), Source: "FxTwitter"}
	id, err := postID(raw)
	if err != nil {
		return p
	}
	if base == "" {
		base = socialEndpoint
	}
	p.URL = "https://x.com/i/status/" + id
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/status/"+id, nil)
	if err != nil {
		p.Status = "invalid_response"
		return p
	}
	req.Header.Set("User-Agent", "gektor-social-reader/1.0 (+https://github.com/keshon/gektor)")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		p.Status = "transient_error"
		return p
	}
	defer resp.Body.Close()
	code := resp.StatusCode
	if code != http.StatusOK {
		p.Status = accessStatus(code)
		return p
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		p.Status = "transient_error"
		return p
	}
	if len(body) > 2<<20 {
		p.Status = "invalid_response"
		return p
	}
	var result struct {
		Code  int `json:"code"`
		Tweet *struct {
			URL       string `json:"url"`
			Text      string `json:"text"`
			CreatedAt string `json:"created_at"`
			Author    struct {
				Name       string `json:"name"`
				ScreenName string `json:"screen_name"`
			} `json:"author"`
			Media struct {
				Photos []struct {
					URL string `json:"url"`
				} `json:"photos"`
				Videos []struct {
					URL string `json:"url"`
				} `json:"videos"`
			} `json:"media"`
		} `json:"tweet"`
	}
	if json.Unmarshal(body, &result) != nil {
		p.Status = "invalid_response"
		return p
	}
	if result.Code != 200 {
		p.Status = accessStatus(result.Code)
		return p
	}
	if result.Tweet == nil || result.Tweet.Author.ScreenName == "" {
		p.Status = "invalid_response"
		return p
	}
	t := result.Tweet
	p.Status, p.Author, p.Handle, p.Text, p.CreatedAt = "ok", t.Author.Name, t.Author.ScreenName, t.Text, t.CreatedAt
	if canonicalID, err := postID(t.URL); err == nil && canonicalID == id && len(t.URL) < 2048 {
		p.URL = t.URL
	}
	for _, media := range t.Media.Photos {
		p.Media = append(p.Media, media.URL)
	}
	for _, media := range t.Media.Videos {
		p.Media = append(p.Media, media.URL)
	}
	// Keep the protocol result below the host's model-output limit; signal partial data.
	for {
		encoded, _ := json.Marshal(p)
		if len(encoded) <= 40*1024 {
			break
		}
		p.Truncated, p.Status = true, "truncated"
		if len(p.Media) > 0 {
			p.Media = p.Media[:len(p.Media)-1]
		} else if len(p.Text) > 0 {
			text := []rune(p.Text)
			p.Text = string(text[:len(text)/2])
		} else {
			p.Author, p.Handle, p.CreatedAt = "", "", ""
		}
	}
	return p
}

func accessStatus(code int) string {
	switch {
	case code == 401 || code == 403:
		return "access_restricted"
	case code == 404 || code == 410:
		return "unavailable"
	case code == 429 || code >= 500:
		return "transient_error"
	default:
		return "invalid_response"
	}
}
