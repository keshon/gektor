// Package httptransport routes HTTP requests through the configured proxy.
// NetworkClient checks destinations and connects through proxies using locally
// validated IP addresses. Transport serves operator-configured endpoints.
package httptransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// ErrDestinationDenied marks URL or resolved-address policy rejection.
var ErrDestinationDenied = errors.New("HTTP destination denied")

// Client validates request and redirect URLs. Its default transport also checks resolved addresses.
func Client(allowLoopback bool, override http.RoundTripper) *http.Client {
	if override == nil {
		override = transportForPolicy(allowLoopback)
	}
	return &http.Client{
		Transport: &validatedTransport{override, allowLoopback},
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			_, err := normalizeURL(req.URL.String(), allowLoopback)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrDestinationDenied, err)
			}
			return nil
		},
	}
}

func NetworkClient(allowLoopback bool) *http.Client { return Client(allowLoopback, nil) }

func newNetworkTransport(allowLoopback bool) *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialTLSContext = nil
	direct := &net.Dialer{Timeout: 10 * time.Second}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialPublic(ctx, network, address, allowLoopback, net.DefaultResolver.LookupIPAddr, direct.DialContext)
	}
	return tr
}

func dialPublic(ctx context.Context, network, address string, allowLoopback bool, lookup func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := lookup(ctx, host)
	if err != nil {
		return nil, err
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}

	for _, ip := range ips {
		if !isPublicIP(ip.IP) && !(allowLoopback && ip.IP.IsLoopback()) {
			return nil, fmt.Errorf("%w: refused non-public address for %s", ErrDestinationDenied, host)
		}
	}

	// Dial the validated literal, never resolve the hostname a second time.
	for _, ip := range ips {
		conn, e := dial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if e == nil {
			return conn, nil
		}
		err = e
	}

	return nil, err
}

type validatedTransport struct {
	inner         http.RoundTripper
	allowLoopback bool
}

func (t *validatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	_, err := normalizeURL(req.URL.String(), t.allowLoopback)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDestinationDenied, err)
	}
	return t.inner.RoundTrip(req)
}
