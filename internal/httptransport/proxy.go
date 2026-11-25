package httptransport

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

type ProxyType string

const (
	ProxyNone   ProxyType = ""
	ProxyHTTP   ProxyType = "http"
	ProxyHTTPS  ProxyType = "https"
	ProxySOCKS5 ProxyType = "socks5"
)

type ProxyConfig struct {
	Type       ProxyType
	Host, Port string
}

var activeProxy atomic.Pointer[ProxyConfig]

// SetProxyConfig sets the preference for subsequently constructed clients.
func SetProxyConfig(cfg ProxyConfig) { activeProxy.Store(&cfg) }

func ActiveProxy() ProxyConfig {
	if cfg := activeProxy.Load(); cfg != nil {
		return *cfg
	}
	return ProxyConfig{}
}

func proxySelector() func(*url.URL) (*url.URL, error) {
	env := httpproxy.FromEnvironment()
	cfg := ActiveProxy()
	if cfg.Type != ProxyNone {
		address := url.URL{Scheme: string(cfg.Type), Host: net.JoinHostPort(cfg.Host, cfg.Port)}
		env.HTTPProxy, env.HTTPSProxy = address.String(), address.String()
		env.CGI = false
	} else {
		all := os.Getenv("ALL_PROXY")
		if all == "" {
			all = os.Getenv("all_proxy")
		}
		if env.HTTPProxy == "" {
			env.HTTPProxy = all
		}
		if env.HTTPSProxy == "" {
			env.HTTPSProxy = all
		}
	}
	selectProxy := env.ProxyFunc()
	return func(target *url.URL) (*url.URL, error) {
		upstream, err := selectProxy(target)
		if err != nil || upstream == nil {
			return upstream, err
		}
		switch upstream.Scheme {
		case "http", "https", "socks5", "socks5h":
			return upstream, nil
		default:
			return nil, fmt.Errorf("unsupported proxy scheme %q", upstream.Scheme)
		}
	}
}

// Transport applies proxy settings to operator-configured service endpoints.
func Transport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	tr := base.Clone()
	ConfigureProxy(tr)
	return tr
}

// ConfigureProxy applies launch settings to an owned service transport.
func ConfigureProxy(tr *http.Transport) {
	selectProxy := proxySelector()
	tr.Proxy = func(req *http.Request) (*url.URL, error) { return selectProxy(req.URL) }
}

type policyTransport struct {
	direct        *http.Transport
	selectProxy   func(*url.URL) (*url.URL, error)
	allowLoopback bool
	mu            sync.Mutex
	routes        map[string]*http.Transport
}

var (
	publicDirect   = newNetworkTransport(false)
	loopbackDirect = newNetworkTransport(true)
)

func transportForPolicy(allowLoopback bool) *policyTransport {
	direct := publicDirect
	if allowLoopback {
		direct = loopbackDirect
	}
	return &policyTransport{
		direct: direct, selectProxy: proxySelector(),
		allowLoopback: allowLoopback, routes: make(map[string]*http.Transport),
	}
}

func (t *policyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	upstream, err := t.selectProxy(req.URL)
	if err != nil {
		return nil, err
	}
	if upstream == nil {
		return t.direct.RoundTrip(req)
	}
	tr := t.route(upstream)
	return tr.RoundTrip(req)
}

func (t *policyTransport) route(upstream *url.URL) *http.Transport {
	key := upstream.String()
	t.mu.Lock()
	defer t.mu.Unlock()
	if tr := t.routes[key]; tr != nil {
		return tr
	}
	tr := t.direct.Clone()
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialPublic(ctx, network, address, t.allowLoopback, net.DefaultResolver.LookupIPAddr,
			func(ctx context.Context, network, target string) (net.Conn, error) {
				return dialProxy(ctx, upstream, network, target)
			})
	}
	t.routes[key] = tr
	return tr
}

func dialProxy(ctx context.Context, upstream *url.URL, network, target string) (net.Conn, error) {
	dial := &net.Dialer{Timeout: 10 * time.Second}
	port := upstream.Port()
	if upstream.Scheme == "socks5" || upstream.Scheme == "socks5h" {
		if port == "" {
			port = "1080"
		}
		var auth *proxy.Auth
		if upstream.User != nil {
			pass, _ := upstream.User.Password()
			auth = &proxy.Auth{User: upstream.User.Username(), Password: pass}
		}
		socks, err := proxy.SOCKS5("tcp", net.JoinHostPort(upstream.Hostname(), port), auth, dial)
		if err != nil {
			return nil, err
		}
		return socks.(proxy.ContextDialer).DialContext(ctx, network, target)
	}
	return connectProxy(ctx, dial, upstream, target)
}

type proxyConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (c proxyConnection) Read(p []byte) (int, error) { return c.reader.Read(p) }

func connectProxy(ctx context.Context, dial *net.Dialer, upstream *url.URL, target string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	port := upstream.Port()
	if port == "" {
		port = "80"
		if upstream.Scheme == "https" {
			port = "443"
		}
	}
	address := net.JoinHostPort(upstream.Hostname(), port)
	dialContext := dial.DialContext
	if upstream.Scheme == "https" {
		secure := &tls.Dialer{NetDialer: dial}
		dialContext = secure.DialContext
	}
	conn, err := dialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return nil, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { conn.Close(); close(done) })
	succeeded := false
	defer func() {
		if !stop() {
			<-done
		}
		if !succeeded {
			conn.Close()
		}
	}()
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if upstream.User != nil {
		pass, _ := upstream.User.Password()
		req.SetBasicAuth(upstream.User.Username(), pass)
		req.Header.Set("Proxy-Authorization", req.Header.Get("Authorization"))
		req.Header.Del("Authorization")
	}
	if err = req.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT returned HTTP %d", response.StatusCode)
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	succeeded = true
	return proxyConnection{Conn: conn, reader: reader}, nil
}
