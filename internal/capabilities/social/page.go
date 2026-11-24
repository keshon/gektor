package social

import (
	"encoding/json"
	"fmt"
	"strings"
)

type providerPage struct {
	Code    int             `json:"code"`
	Status  *providerPost   `json:"status"`
	Results *[]providerPost `json:"results"`
	Thread  []providerPost  `json:"thread"`
	Replies []providerPost  `json:"replies"`
	Trends  *[]Trend        `json:"trends"`
	Cursor  struct {
		Bottom string `json:"bottom"`
	} `json:"cursor"`
}

func (p *Page) populate(result providerPage, in PageRequest, kind string) {
	p.Status = "ok"
	// Providers may ignore count. Preserve their page so its cursor does not skip omitted posts.
	limit := 100

	if result.Status != nil {
		post := result.Status.post(p.RetrievedAt)
		p.Post = &post
		if post.Status != "ok" {
			p.Status = "partial"
		}
	}
	if result.Results != nil {
		p.Posts = p.posts(*result.Results, limit)
	}
	p.Thread = p.posts(result.Thread, limit)
	p.Replies = p.posts(result.Replies, limit)
	if result.Trends != nil {
		p.Trends = *result.Trends
		if len(p.Trends) > in.Limit {
			p.Trends = p.Trends[:in.Limit]
			p.Truncated = true
		}
	}
	for _, trend := range p.Trends {
		if strings.TrimSpace(trend.Name) == "" {
			p.Status = "partial"
		}
	}
	if kind != ReadTrends && result.Cursor.Bottom != "" {
		if len(result.Cursor.Bottom) > 4096 || strings.ContainsAny(result.Cursor.Bottom, "\r\n") || result.Cursor.Bottom == in.Cursor {
			p.Truncated = true
		} else {
			p.NextCursor = result.Cursor.Bottom
			p.More = true
		}
	}
}

func (p *Page) posts(items []providerPost, limit int) []Post {
	if len(items) > limit {
		items = items[:limit]
		p.Truncated = true
	}
	posts := make([]Post, 0, len(items))
	for _, item := range items {
		post := item.post(p.RetrievedAt)
		if post.Status != "ok" {
			p.Status = "partial"
		}
		posts = append(posts, post)
	}
	return posts
}

func (p *Page) bound() error {
	// Leave headroom below the tool-output ceiling. Truncation never implies a resumable offset.
	for {
		encoded, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if len(encoded) <= 40*1024-16 {
			break
		}
		p.Truncated = true
		switch {
		case len(p.Replies) > 0:
			p.Replies = p.Replies[:len(p.Replies)-1]
		case len(p.Thread) > 0:
			p.Thread = p.Thread[:len(p.Thread)-1]
		case len(p.Posts) > 0:
			p.Posts = p.Posts[:len(p.Posts)-1]
		case len(p.Trends) > 0:
			p.Trends = p.Trends[:len(p.Trends)-1]
		case p.Post != nil:
			p.Post = nil
		default:
			return fmt.Errorf("social response cannot fit output budget")
		}
	}
	return nil
}
