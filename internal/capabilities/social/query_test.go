package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const fixturePost = `{"id":"20","url":"https://x.com/test/status/20","text":"fixture post","author":{"screen_name":"test"}}`

func TestPageRequestsAndPagination(t *testing.T) {
	for _, tc := range []struct {
		kind              string
		request           PageRequest
		path, query, body string
	}{
		{ReadThread, PageRequest{URL: "https://twitter.com/test/status/20", Cursor: "a+/=&?"}, "/2/conversation/20", "ranking_mode", `{"code":200,"status":` + fixturePost + `,"thread":[` + fixturePost + `],"replies":[` + fixturePost + `],"cursor":{"bottom":"next"}}`},
		{ReadTimeline, PageRequest{Handle: "@Test", Limit: 2, Cursor: "a+/=&?"}, "/2/profile/test/statuses", "count", `{"code":200,"results":[` + fixturePost + `],"cursor":{"bottom":"next"}}`},
		{ReadTrends, PageRequest{Limit: 2}, "/2/trends", "count", `{"code":200,"trends":[{"name":"topic","rank":null,"context":null}],"cursor":{"bottom":"ignored"}}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != tc.path || r.URL.Query().Get(tc.query) == "" || r.URL.Query().Get("cursor") != tc.request.Cursor {
					t.Errorf("wrong request: %s", r.URL)
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			service := Service{Client: server.Client(), Endpoint: server.URL}
			page, err := service.Query(t.Context(), tc.kind, tc.request)
			if err != nil || page.Status != "ok" || requests != 1 {
				t.Fatal(page, err, requests)
			}
			if tc.kind == ReadTrends {
				if page.More || page.NextCursor != "" || len(page.Trends) != 1 {
					t.Fatal(page)
				}
			} else if !page.More || page.NextCursor != "next" {
				t.Fatal(page)
			}
		})
	}
}

func TestPageFailuresAreNotEmptySuccesses(t *testing.T) {
	for _, tc := range []struct {
		code         int
		body, status string
	}{
		{404, ``, "unavailable"}, {403, ``, "access_restricted"}, {429, ``, "transient_error"}, {503, ``, "transient_error"},
		{200, `{"code":404,"results":[]}`, "unavailable"},
		{200, `{"code":200}`, "invalid_response"}, {200, `<html>gateway</html>`, "invalid_response"},
		{200, `{"code":200,"results":[{}]}`, "partial"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) }))
		service := Service{Client: server.Client(), Endpoint: server.URL}
		page, err := service.Query(t.Context(), ReadTimeline, PageRequest{Handle: "test"})
		server.Close()
		if err == nil || page.Status != tc.status {
			t.Fatal(tc, page, err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"results":[],"cursor":{"bottom":null}}`)
	}))
	defer server.Close()
	page, err := (Service{Client: server.Client(), Endpoint: server.URL}).Query(t.Context(), ReadTimeline, PageRequest{Handle: "test"})
	if err != nil || page.Status != "ok" || page.More {
		t.Fatal(page, err)
	}
}

func TestPageValidationAndCancellationAvoidNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	service := Service{Client: server.Client(), Endpoint: server.URL}
	for _, tc := range []struct {
		kind string
		in   PageRequest
	}{
		{ReadThread, PageRequest{URL: "https://evil.test/x/status/20"}},
		{ReadTimeline, PageRequest{Handle: "../admin"}}, {ReadTimeline, PageRequest{Handle: "https://x.com/test"}},
		{ReadTimeline, PageRequest{Handle: "test", Limit: 21}}, {ReadTimeline, PageRequest{Handle: "test", Cursor: strings.Repeat("x", 4097)}},
		{ReadTrends, PageRequest{Cursor: "next"}}, {ReadThread, PageRequest{URL: "https://x.com/test/status/20", Limit: 1}},
		{"search", PageRequest{}},
	} {
		if _, err := service.Query(t.Context(), tc.kind, tc.in); err == nil {
			t.Fatal("accepted invalid page request", tc)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := service.Query(ctx, ReadTrends, PageRequest{})
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(err, calls)
	}
}

func TestPagesPreserveProviderReplyPageAndFlagLoss(t *testing.T) {
	replies := strings.TrimSuffix(strings.Repeat(fixturePost+",", 34), ",")
	body := `{"code":200,"status":` + fixturePost + `,"thread":[{"type":"tombstone","message":"deleted"}],"replies":[` + replies + `],"cursor":{"bottom":"next"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer server.Close()
	service := Service{Client: server.Client(), Endpoint: server.URL}
	page, err := service.Query(t.Context(), ReadThread, PageRequest{URL: "https://x.com/test/status/20"})
	if err == nil || page.Status != "partial" || len(page.Replies) != 34 || len(page.Thread) != 1 || page.Thread[0].Text != "deleted" || !page.More || page.Truncated {
		t.Fatal(page, err)
	}
	body = `{"code":200,"results":[` + fixturePost + `,` + fixturePost + `],"cursor":{"bottom":"same"}}`
	page, err = service.Query(t.Context(), ReadTimeline, PageRequest{Handle: "test", Limit: 1, Cursor: "same"})
	if err == nil || !page.Truncated || page.More || len(page.Posts) != 2 {
		t.Fatal(page, err)
	}
	oversized := strings.Replace(fixturePost, "fixture post", strings.Repeat("я", 50000), 1)
	body = `{"code":200,"status":` + fixturePost + `,"replies":[` + oversized + `],"thread":[]}`
	page, err = service.Query(t.Context(), ReadThread, PageRequest{URL: "https://x.com/test/status/20"})
	encoded, encodeErr := json.Marshal(page)
	if err == nil || !page.Truncated || len(encoded) > 40*1024 || encodeErr != nil || page.Post == nil {
		t.Fatal(page.Status, len(encoded), err, encodeErr)
	}
	body = strings.Repeat("x", (2<<20)+1)
	page, err = service.Query(t.Context(), ReadTrends, PageRequest{})
	if err == nil || page.Status != "invalid_response" {
		t.Fatal(page, err)
	}
}

func TestSocialPageRedirectIsNotFollowed(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	page, err := (Service{Client: server.Client(), Endpoint: server.URL}).Query(t.Context(), ReadTrends, PageRequest{})
	if err == nil || page.Status != "invalid_response" || calls != 0 {
		t.Fatal(page, err, calls)
	}
}

func TestSocialPagesLive(t *testing.T) {
	if os.Getenv("GEKTOR_SOCIAL_LIVE") != "1" {
		t.Skip("set GEKTOR_SOCIAL_LIVE=1 for public provider requests")
	}
	for _, tc := range []struct {
		kind string
		in   PageRequest
	}{
		{ReadThread, PageRequest{URL: "https://x.com/jack/status/20"}},
		{ReadTrends, PageRequest{Limit: 2}},
		{ReadTimeline, PageRequest{Handle: "SpaceX", Limit: 2}},
	} {
		page, err := New().Query(t.Context(), tc.kind, tc.in)
		if err != nil && page.Status == "invalid_response" {
			t.Fatal(tc.kind, page.Status, err)
		}
		t.Logf("%s status=%s posts=%d thread=%d replies=%d trends=%d more=%t error=%v", tc.kind, page.Status, len(page.Posts), len(page.Thread), len(page.Replies), len(page.Trends), page.More, err)
	}
}
