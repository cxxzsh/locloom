package netpool

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/netproxy"
	"golang.org/x/net/proxy"
)

const defaultTimeout = 60 * time.Second

// ProxyConfig configures direct and proxied HTTP client behavior.
type ProxyConfig struct {
	ProxyURL      string        `json:"proxy_url"`
	SkipTLSVerify bool          `json:"skip_tls_verify"`
	Timeout       time.Duration `json:"timeout,omitempty"`
}

// Pool manages thread-safe direct and proxied *http.Client instances with connection pool reuse.
type Pool struct {
	mu           sync.Mutex
	cfg          ProxyConfig
	directClient *http.Client
	directTr     *http.Transport
	proxyClient  *http.Client
	proxyTr      *http.Transport
	proxyBuilt   bool
	proxyErr     error
}

// New creates a Pool with the provided configuration.
// Zero or negative Timeout defaults to 60 seconds.
func New(cfg ProxyConfig) *Pool {
	return &Pool{cfg: normalizeConfig(cfg)}
}

// Update replaces the pool configuration dynamically and closes idle connections on rotated transports.
func (p *Pool) Update(cfg ProxyConfig) {
	norm := normalizeConfig(cfg)

	p.mu.Lock()
	if norm == p.cfg {
		p.mu.Unlock()
		return
	}
	oldDirect := p.directTr
	oldProxy := p.proxyTr
	p.cfg = norm
	p.directClient = nil
	p.directTr = nil
	p.proxyClient = nil
	p.proxyTr = nil
	p.proxyBuilt = false
	p.proxyErr = nil
	p.mu.Unlock()

	if oldDirect != nil {
		oldDirect.CloseIdleConnections()
	}
	if oldProxy != nil {
		oldProxy.CloseIdleConnections()
	}
}

// Config returns a copy of the current pool configuration.
func (p *Pool) Config() ProxyConfig {
	p.mu.Lock()
	cfg := p.cfg
	p.mu.Unlock()
	return cfg
}

// Client returns a cached *http.Client configured either with the explicit proxy (useProxy=true)
// or with environment / OS system proxy fallback via netproxy (useProxy=false).
func (p *Pool) Client(useProxy bool) (*http.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !useProxy {
		if p.directClient == nil {
			tr := newBaseTransport(p.cfg.SkipTLSVerify)
			tr.Proxy = netproxy.Proxy
			p.directTr = tr
			p.directClient = &http.Client{Transport: tr, Timeout: p.cfg.Timeout}
		}
		return p.directClient, nil
	}

	if !p.proxyBuilt {
		c, tr, err := buildProxyClient(p.cfg)
		p.proxyClient = c
		p.proxyTr = tr
		p.proxyErr = err
		p.proxyBuilt = true
	}
	return p.proxyClient, p.proxyErr
}

func newBaseTransport(skipTLS bool) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: skipTLS, //nolint:gosec // User-controlled setting for local/custom TLS gateways
		},
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func buildProxyClient(cfg ProxyConfig) (*http.Client, *http.Transport, error) {
	rawProxy := cfg.ProxyURL
	if rawProxy == "" {
		return nil, nil, fmt.Errorf("proxy URL is empty")
	}
	if !strings.Contains(rawProxy, "://") {
		rawProxy = "http://" + rawProxy
	}
	u, err := url.Parse(rawProxy)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid proxy URL %q: %w", rawProxy, err)
	}

	tr := newBaseTransport(cfg.SkipTLSVerify)
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		tr.Proxy = http.ProxyURL(u)
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{
				User:     u.User.Username(),
				Password: pw,
			}
		}
		socksDialer, err := proxy.SOCKS5("tcp", u.Host, auth, dialer)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to initialize SOCKS5 dialer: %w", err)
		}
		tr.Proxy = nil
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if cd, ok := socksDialer.(proxy.ContextDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return socksDialer.Dial(network, addr)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported proxy scheme %q (supported: http, https, socks5, socks5h)", u.Scheme)
	}

	return &http.Client{Transport: tr, Timeout: cfg.Timeout}, tr, nil
}

func normalizeConfig(cfg ProxyConfig) ProxyConfig {
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return cfg
}
