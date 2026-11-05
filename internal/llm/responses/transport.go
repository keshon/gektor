package responses

import (
	"net/http"
	"time"
)

// Connection and header waits are independent of long streamed generation.
func responseHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 15 * time.Second
	return &http.Client{Timeout: 120 * time.Minute, Transport: transport}
}

func (c *Client) idleTimeout() time.Duration {
	if c.streamIdleTimeout > 0 {
		return c.streamIdleTimeout
	}
	return 5 * time.Minute
}
