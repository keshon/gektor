// Package social reads public posts through bounded provider clients.
package social

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/keshon/gektor/internal/httptransport"
)

type Request struct {
	URL string `json:"url"`
}
type Service struct {
	Client   *http.Client
	Endpoint string
}

func New() Service {
	client := httptransport.NetworkClient(false)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return Service{Client: client}
}
func (s Service) Validate(in Request) (Request, error) {
	if len(in.URL) > 2048 {
		return in, fmt.Errorf("post URL exceeds 2048 bytes")
	}
	return in, nil
}
func (s Service) Read(ctx context.Context, in Request) (Post, error) {
	in, err := s.Validate(in)
	if err != nil {
		return Post{}, err
	}
	if s.Client == nil {
		return Post{}, fmt.Errorf("social HTTP client unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := ReadPost(ctx, s.Client, s.Endpoint, in.URL)
	if err := ctx.Err(); err != nil {
		return p, fmt.Errorf("read post: %w", err)
	}
	if p.Status != "ok" {
		return p, fmt.Errorf("%s: post retrieval incomplete; report this limitation", p.Status)
	}
	return p, nil
}
