package extension

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/httptransport"
	"github.com/keshon/gektor/internal/mcp"
)

// StartMCPHTTP initializes an authorized endpoint and adapts its catalog.
// The caller must Close the returned session.
func StartMCPHTTP(ctx context.Context, server, rawURL string) ([]agent.Tool, *Session, error) {
	return StartMCPHTTPWithHeaders(ctx, server, rawURL, nil)
}

// StartMCPHTTPWithHeaders binds credentials to HTTPS and refuses redirects.
func StartMCPHTTPWithHeaders(ctx context.Context, server, rawURL string, headers map[string]string) ([]agent.Tool, *Session, error) {
	var secrets []string
	for name, value := range headers {
		invalid := !validMCPHeader(name) || strings.ContainsAny(value, "\r\n") || value == ""
		if invalid {
			return nil, nil, fmt.Errorf("invalid MCP header")
		}
		secrets = append(secrets, value)
		if strings.HasPrefix(value, "Bearer ") {
			secrets = append(secrets, strings.TrimPrefix(value, "Bearer "))
		}
	}
	if len(headers) > 0 && !strings.HasPrefix(rawURL, "https://") {
		return nil, nil, fmt.Errorf("authenticated MCP requires HTTPS")
	}
	err := validateCapabilityID(server)
	if err != nil {
		return nil, nil, err
	}
	clean, err := httptransport.Normalize(rawURL, true)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp %s: %w", server, err)
	}

	client := httptransport.Client(true, nil)
	if len(headers) > 0 {
		// net/http requires this callback to prevent credential-bearing redirects.
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return fmt.Errorf("authenticated MCP redirects are not allowed")
		}
	}
	client.Timeout = 0 // Each operation owns its deadline.
	discovery, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cfg := mcp.HTTPConfig{
		Server:  server,
		URL:     clean,
		Client:  client,
		Headers: headers,
	}
	transport, defs, err := mcp.NewHTTP(discovery, cfg)
	if err != nil {
		return nil, nil, redactCredentialError(err, secrets)
	}

	session := &Session{transport: transport, secrets: secrets}
	list := adaptMCPTools(server, session, defs, secrets)
	return list, session, nil
}

// IsMCPURL reports whether s is an http(s) URL (an MCP server address)
// rather than a command line.
func IsMCPURL(s string) bool {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return false
	}
	lower := strings.ToLower(fields[0])
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func validMCPHeader(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	switch strings.ToLower(name) {
	case "host", "content-type", "accept", "content-length", "connection", "cookie", "transfer-encoding", "mcp-session-id", "mcp-protocol-version":
		return false
	}
	return true
}
