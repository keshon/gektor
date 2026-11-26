package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/httptransport"
)

// SearchResult represents a single search result found by an engine.
type Result struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

// Search retrieves results from the selected web search engine.
type Search struct{}

const defaultSearchEngine = "duckduckgo"

var searchEngines = map[string]func(context.Context, string) ([]Result, error){
	defaultSearchEngine: searchDuckDuckGo,
	"wikipedia":         searchWikipedia,
}

func searchEngineNames() string {
	return strings.Join(slices.Sorted(maps.Keys(searchEngines)), ", ")
}

func (Search) Name() string         { return "search_web" }
func (Search) Mode() agent.ToolMode { return agent.Concurrent }
func (Search) Description() string {
	return "Search the web or Wikipedia. Returns titles, snippets, and URLs. Engines: " + searchEngineNames() + "."
}
func (Search) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"query": {"type": "string"},
			"engine": {"type": "string", "description": "search engine to use (%s). Default is %s."}
		},
		"required": ["query"]
	}`, searchEngineNames(), defaultSearchEngine))
}

func (Search) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query  string `json:"query"`
		Engine string `json:"engine"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := agent.RejectUnknownFields(args, "query", "engine"); err != nil {
		return "", err
	}

	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return "", fmt.Errorf("search query is empty")
	}

	engine := strings.ToLower(strings.TrimSpace(in.Engine))
	if engine == "" {
		engine = defaultSearchEngine
	}
	search, ok := searchEngines[engine]
	if !ok {
		return "", fmt.Errorf("unsupported engine: %s (supported: %s)", engine, searchEngineNames())
	}
	results, err := search(ctx, in.Query)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "SEARCH_WEB\nengine: " + engine + "\nquery: " + in.Query + "\n(no results found)", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "SEARCH_WEB\nengine: %s\nquery: %s\nresults: %d\n----\n", engine, in.Query, len(results))
	for i, res := range results {
		fmt.Fprintf(&b, "[%d] %s\n    URL: %s\n    Snippet: %s\n\n", i+1, res.Title, res.URL, res.Snippet)
	}

	return b.String(), nil
}

func searchDuckDuckGo(ctx context.Context, query string) ([]Result, error) {
	searchURL := "https://duckduckgo.com/html/?q=" + url.QueryEscape(query)
	fetchArgs, _ := json.Marshal(map[string]string{"url": searchURL})
	content, err := (Raw{}).Run(ctx, fetchArgs)
	if err != nil {
		return nil, err
	}
	if strings.Contains(content, `data-testid="anomaly-modal"`) {
		return nil, fmt.Errorf("DuckDuckGo returned a bot challenge")
	}
	return parseDuckDuckGo(content), nil
}

func searchWikipedia(ctx context.Context, query string) ([]Result, error) {
	return queryWikipedia(ctx, query, httptransport.NetworkClient(false))
}

func queryWikipedia(ctx context.Context, query string, client *http.Client) ([]Result, error) {
	const base = "https://en.wikipedia.org"
	params := url.Values{
		"action": {"query"}, "list": {"search"}, "format": {"json"}, "formatversion": {"2"},
		"srsearch": {query}, "srlimit": {"10"}, "srprop": {"snippet"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/w/api.php?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Gektor/1.0 (https://github.com/keshon/gektor)")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Wikipedia: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Wikipedia search returned HTTP %d", resp.StatusCode)
	}

	const maxResponse = 256 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read Wikipedia search: %w", err)
	}
	if len(body) > maxResponse {
		return nil, fmt.Errorf("Wikipedia search response exceeds 256 KiB")
	}
	var response struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
		Query *struct {
			Search []struct {
				PageID  int    `json:"pageid"`
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, fmt.Errorf("decode Wikipedia search: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("Wikipedia search API error: %q", response.Error.Code)
	}
	if response.Query == nil {
		return nil, fmt.Errorf("Wikipedia search response is missing results")
	}

	var results []Result
	for _, page := range response.Query.Search {
		if page.PageID <= 0 || strings.TrimSpace(page.Title) == "" {
			continue
		}
		results = append(results, Result{
			Title: collapseSpace(page.Title), Snippet: collapseSpace(stripHTML(page.Snippet)),
			URL: fmt.Sprintf("%s/?curid=%d", base, page.PageID),
		})
		if len(results) == 10 {
			break
		}
	}
	return results, nil
}

// parseDuckDuckGo parses the duckduckgo.com/html/ endpoint. Its real
// structure (verified 2026-10): result links are
// <a class="result__a" href="//duckduckgo.com/l/?uddg=<real-url>...>,
// snippets are <a class="result__snippet">. The /l/ wrapper must be
// unwrapped, not filtered — dropping duckduckgo.com links drops every
// result, which is how this parser shipped returning nothing live.
func parseDuckDuckGo(content string) []Result {
	linkRe := regexp.MustCompile(`<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	snipRe := regexp.MustCompile(`<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)

	linkMatches := linkRe.FindAllStringSubmatch(content, -1)
	snipMatches := snipRe.FindAllStringSubmatch(content, -1)

	var results []Result
	for i, m := range linkMatches {
		urlStr := unwrapDDGLink(m[1])
		if urlStr == "" {
			continue
		}
		// stripHTML emits a newline at every tag boundary, so inline
		// markup ("Title <b>1</b>") comes out multiline — collapse to
		// one line for result display.
		title := collapseSpace(stripHTML(m[2]))
		if title == "" {
			continue
		}
		var snippet string
		if i < len(snipMatches) {
			snippet = collapseSpace(stripHTML(snipMatches[i][1]))
		}
		duplicate := false
		for _, r := range results {
			if r.URL == urlStr {
				duplicate = true
				break
			}
		}
		if !duplicate {
			results = append(results, Result{Title: title, URL: urlStr, Snippet: snippet})
		}
		if len(results) >= 10 {
			break
		}
	}

	return results
}

// collapseSpace trims and collapses all whitespace runs to single
// spaces: extracted result fields must render on one line.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// unwrapDDGLink resolves a result href to the target URL: /l/?uddg=...
// wrappers decode to the real page, bare http(s) links pass through,
// anything else (internal DDG pages, nav) is rejected with "".
func unwrapDDGLink(href string) string {
	href = strings.ReplaceAll(href, "&amp;", "&")
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	if !strings.HasPrefix(href, "http") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if strings.Contains(u.Host, "duckduckgo.com") {
		real := u.Query().Get("uddg")
		if real == "" || (!strings.HasPrefix(real, "http://") && !strings.HasPrefix(real, "https://")) {
			return ""
		}
		return real
	}
	return href
}
