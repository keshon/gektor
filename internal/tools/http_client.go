package tools

import (
	"github.com/keshon/gektor/internal/integrations/httptransport"
	"net/http"
)

func networkClient(loopback bool, override http.RoundTripper) *http.Client {
	return httptransport.Client(loopback, override)
}

// NetworkClient enforces public destinations, optionally permitting local dev servers.
func NetworkClient(loopback bool) *http.Client { return networkClient(loopback, nil) }
