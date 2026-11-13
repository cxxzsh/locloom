package netpool

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPoolConfigDefaultsAndUpdate(t *testing.T) {
	p := New(ProxyConfig{ProxyURL: "  http://127.0.0.1:8080  "})
	cfg := p.Config()
	if cfg.ProxyURL != "http://127.0.0.1:8080" {
		t.Fatalf("expected trimmed proxy URL, got %q", cfg.ProxyURL)
	}
	if cfg.Timeout != 60*time.Second {
		t.Fatalf("expected default timeout 60s, got %v", cfg.Timeout)
	}

	p.Update(ProxyConfig{
		ProxyURL:      "socks5h://127.0.0.1:1080",
		SkipTLSVerify: true,
		Timeout:       15 * time.Second,
	})
	cfg = p.Config()
	if cfg.ProxyURL != "socks5h://127.0.0.1:1080" || !cfg.SkipTLSVerify || cfg.Timeout != 15*time.Second {
		t.Fatalf("unexpected updated config: %+v", cfg)
	}
}

func TestPoolDirectClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "ok-direct")
	}))
	defer srv.Close()

	p := New(ProxyConfig{ProxyURL: "http://127.0.0.1:1"}) // unreachable proxy should not be used
	client, err := p.Client(false)
	if err != nil {
		t.Fatalf("Client(false) error: %v", err)
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("direct GET failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok-direct" {
		t.Fatalf("unexpected body %q", string(body))
	}
}

func TestPoolHTTPProxyAndErrors(t *testing.T) {
	p := New(ProxyConfig{})
	if _, err := p.Client(true); err == nil {
		t.Fatal("expected error for empty proxy URL")
	}

	p.Update(ProxyConfig{ProxyURL: "ftp://127.0.0.1:2121"})
	if _, err := p.Client(true); err == nil {
		t.Fatal("expected error for unsupported proxy scheme")
	}

	var proxiedHost string
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedHost = r.Host
		_, _ = fmt.Fprint(w, "via-proxy")
	}))
	defer proxySrv.Close()

	p.Update(ProxyConfig{ProxyURL: proxySrv.URL})
	client, err := p.Client(true)
	if err != nil {
		t.Fatalf("Client(true) error: %v", err)
	}
	resp, err := client.Get("http://example.invalid/ping")
	if err != nil {
		t.Fatalf("proxied GET failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "via-proxy" || proxiedHost != "example.invalid" {
		t.Fatalf("unexpected proxy response body=%q host=%q", string(body), proxiedHost)
	}
}

func TestPoolSOCKS5AndSkipTLS(t *testing.T) {
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "tls-ok")
	}))
	defer tlsSrv.Close()

	p := New(ProxyConfig{
		ProxyURL:      "socks5h://user:pass@127.0.0.1:1080",
		SkipTLSVerify: true,
		Timeout:       10 * time.Second,
	})

	directClient, err := p.Client(false)
	if err != nil {
		t.Fatalf("Client(false) error: %v", err)
	}
	resp, err := directClient.Get(tlsSrv.URL)
	if err != nil {
		t.Fatalf("TLS GET with SkipTLSVerify failed: %v", err)
	}
	_ = resp.Body.Close()

	socksClient, err := p.Client(true)
	if err != nil {
		t.Fatalf("Client(true) with socks5h error: %v", err)
	}
	if socksClient.Timeout != 10*time.Second {
		t.Fatalf("expected 10s timeout, got %v", socksClient.Timeout)
	}
}

func TestPoolConcurrency(t *testing.T) {
	p := New(ProxyConfig{ProxyURL: "http://127.0.0.1:8080"})
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p.Update(ProxyConfig{
				ProxyURL:      fmt.Sprintf("http://127.0.0.1:%d", 8080+idx),
				SkipTLSVerify: idx%2 == 0,
				Timeout:       time.Duration(idx+1) * time.Second,
			})
			_ = p.Config()
			_, _ = p.Client(idx%2 == 0)
		}(i)
	}
	wg.Wait()
}
