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

	"golang.org/x/net/proxy"
)

const defaultTimeout = 60 * time.Second

// ProxyConfig configures direct and proxied HTTP client behavior.
type ProxyConfig struct {
	ProxyURL      string        `json:"proxy_url"`
	SkipTLSVerify bool          `json:"skip_tls_verify"`
	Timeout       time.Duration `json:"timeout,omitempty"`
}

// Pool manages thread-safe direct and proxied *http.Client instances.
type Pool struct {
	mu  sync.RWMutex
	cfg ProxyConfig
}

// New creates a Pool with the provided configuration.
// Zero or negative Timeout defaults to 60 seconds.
func New(cfg ProxyConfig) *Pool {
	return &Pool{cfg: normalizeConfig(cfg)}
}

// Update replaces the pool configuration dynamically.
func (p *Pool) Update(cfg ProxyConfig) {
	p.mu.Lock()
	p.cfg = normalizeConfig(cfg)
	p.mu.Unlock()
}

// Config returns a copy of the current pool configuration.
func (p *Pool) Config() ProxyConfig {
	p.mu.RLock()
	cfg := p.cfg
	p.mu.RUnlock()
	return cfg
}

// Client returns an *http.Client configured either with the configured proxy (useProxy=true)
// or with a direct connection (useProxy=false).
func (p *Pool) Client(useProxy bool) (*http.Client, error) {
	cfg := p.Config()

	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.SkipTLSVerify, //nolint:gosec // User-controlled setting for local/custom TLS gateways
	}

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	tr := &http.Transport{
		TLSClientConfig:       tlsCfg,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if !useProxy {
		tr.Proxy = nil
		return &http.Client{Transport: tr, Timeout: cfg.Timeout}, nil
	}

	rawProxy := cfg.ProxyURL
	if rawProxy == "" {
		return nil, fmt.Errorf("proxy URL is empty")
	}
	if !strings.Contains(rawProxy, "://") {
		rawProxy = "http://" + rawProxy
	}
	u, err := url.Parse(rawProxy)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL %q: %w", rawProxy, err)
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
			return nil, fmt.Errorf("failed to initialize SOCKS5 dialer: %w", err)
		}
		tr.Proxy = nil
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if cd, ok := socksDialer.(proxy.ContextDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return socksDialer.Dial(network, addr)
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (supported: http, https, socks5, socks5h)", u.Scheme)
	}

	return &http.Client{Transport: tr, Timeout: cfg.Timeout}, nil
}

func normalizeConfig(cfg ProxyConfig) ProxyConfig {
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return cfg
}
