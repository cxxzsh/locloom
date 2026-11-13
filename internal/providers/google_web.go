package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"locloom/internal/domain"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

type googleWebProvider struct {
	mu        sync.RWMutex
	useProxy  bool
	host      string
	endpoint  string
	batchSize int
	pool      *netpool.Pool
}

// NewGoogleWebProvider creates the built-in Google Translate Web API provider.
func NewGoogleWebProvider() provider.Provider {
	return &googleWebProvider{
		host:      "translate.googleapis.com",
		endpoint:  "gtx",
		batchSize: 25,
	}
}

func (p *googleWebProvider) ID() string   { return domain.OriginGoogle }
func (p *googleWebProvider) Name() string { return "Google Translate (免Key批量)" }
func (p *googleWebProvider) Description() string {
	return "调用 Google Translate Web 接口批量翻译（支持 gtx / clients5 与可选代理）"
}
func (p *googleWebProvider) Kind() provider.Kind { return provider.KindEngine }

func (p *googleWebProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:     "host",
			Label:   "API 域名",
			Type:    provider.FieldSelect,
			Default: "translate.googleapis.com",
			Options: []string{"translate.googleapis.com", "translate.google.com"},
		},
		{
			Key:     "endpoint",
			Label:   "接口端点",
			Type:    provider.FieldSelect,
			Default: "gtx",
			Options: []string{"gtx", "clients5"},
		},
		{
			Key:     "batch_size",
			Label:   "单批词条数",
			Type:    provider.FieldText,
			Default: "25",
		},
		{
			Key:     "use_proxy",
			Label:   "使用代理",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *googleWebProvider) Configure(values map[string]string, pool *netpool.Pool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool = pool
	if h := strings.TrimSpace(values["host"]); h != "" {
		p.host = h
	} else {
		p.host = "translate.googleapis.com"
	}
	if ep := strings.TrimSpace(values["endpoint"]); ep != "" {
		p.endpoint = ep
	} else {
		p.endpoint = "gtx"
	}
	p.useProxy = values["use_proxy"] == "true"
	if bs, err := strconv.Atoi(strings.TrimSpace(values["batch_size"])); err == nil && bs > 0 && bs <= 100 {
		p.batchSize = bs
	} else {
		p.batchSize = 25
	}
	return nil
}

func (p *googleWebProvider) getClient() (*http.Client, string, string, int, error) {
	p.mu.RLock()
	pool := p.pool
	useProxy := p.useProxy
	host := p.host
	ep := p.endpoint
	bs := p.batchSize
	p.mu.RUnlock()

	if pool == nil {
		pool = netpool.New(netpool.ProxyConfig{})
	}
	client, err := pool.Client(useProxy)
	if err != nil {
		return nil, "", "", 0, err
	}
	return client, host, ep, bs, nil
}

func (p *googleWebProvider) Check(ctx context.Context) error {
	client, host, ep, _, err := p.getClient()
	if err != nil {
		return err
	}
	res, err := callGoogleEndpoint(ctx, client, host, ep, "en", "zh-CN", "Hello, %1!")
	if err != nil {
		return err
	}
	if res == "" {
		return fmt.Errorf("google_web returned empty smoke check result")
	}
	return nil
}

func (p *googleWebProvider) Translate(ctx context.Context, req provider.Request) (map[string]string, error) {
	if len(req.Items) == 0 {
		return map[string]string{}, nil
	}
	client, host, ep, batchSize, err := p.getClient()
	if err != nil {
		return nil, err
	}

	tl := req.EngineCode
	if tl == "" {
		tl = req.TargetLocale.ID
	}
	sl := "auto"

	out := make(map[string]string, len(req.Items))
	for start := 0; start < len(req.Items); start += batchSize {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		end := min(start+batchSize, len(req.Items))
		chunk := req.Items[start:end]

		texts := make([]string, len(chunk))
		for i, it := range chunk {
			if it.Key.Source != "" {
				texts[i] = it.Key.Source
			} else {
				texts[i] = it.RefText
			}
		}

		translated, err := p.translateBatchWithFallback(ctx, client, host, ep, sl, tl, texts)
		if err != nil {
			return out, err
		}
		for i, it := range chunk {
			if i < len(translated) && translated[i] != "" {
				out[it.ID] = translated[i]
			}
		}
	}
	return out, nil
}

func (p *googleWebProvider) translateBatchWithFallback(
	ctx context.Context,
	client *http.Client,
	host, primaryEP, sl, tl string,
	items []string,
) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) == 1 {
		res, err := callGoogleEndpoint(ctx, client, host, primaryEP, sl, tl, items[0])
		if err != nil {
			altEP := "clients5"
			if primaryEP == "clients5" {
				altEP = "gtx"
			}
			res2, err2 := callGoogleEndpoint(ctx, client, host, altEP, sl, tl, items[0])
			if err2 == nil {
				return []string{res2}, nil
			}
			return nil, err
		}
		return []string{res}, nil
	}

	// Only join items that do not contain embedded raw newlines (though guard.go already masks \n as [[N]]).
	canBatch := true
	for _, s := range items {
		if strings.Contains(s, "\n") {
			canBatch = false
			break
		}
	}

	if canBatch {
		joined := strings.Join(items, "\n")
		if len(joined) <= 3500 {
			res, err := callGoogleEndpoint(ctx, client, host, primaryEP, sl, tl, joined)
			if err == nil {
				lines := strings.Split(res, "\n")
				if len(lines) == len(items) {
					for i := range lines {
						// Strip only stray \r, never intentional spaces.
						lines[i] = strings.TrimSuffix(lines[i], "\r")
					}
					return lines, nil
				}
			}
		}
	}

	// Binary split fallback when line counts drift or payload is too large.
	mid := len(items) / 2
	left, err := p.translateBatchWithFallback(ctx, client, host, primaryEP, sl, tl, items[:mid])
	if err != nil {
		return nil, err
	}
	right, err := p.translateBatchWithFallback(ctx, client, host, primaryEP, sl, tl, items[mid:])
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func googleBaseURL(host string) string {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "https://" + host
}

func callGoogleEndpoint(ctx context.Context, client *http.Client, host, ep, sl, tl, text string) (string, error) {
	base := googleBaseURL(host)
	var reqURL string
	switch ep {
	case "clients5":
		reqURL = fmt.Sprintf(
			"%s/translate_a/t?client=dict-chrome-ex&sl=%s&tl=%s&q=%s",
			base, url.QueryEscape(sl), url.QueryEscape(tl), url.QueryEscape(text),
		)
	default: // gtx
		reqURL = fmt.Sprintf(
			"%s/translate_a/single?client=gtx&sl=%s&tl=%s&dt=t&q=%s",
			base, url.QueryEscape(sl), url.QueryEscape(tl), url.QueryEscape(text),
		)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; locloom/2.0)")

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if ep == "clients5" {
		var arr []any
		if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
			switch v := arr[0].(type) {
			case string:
				return v, nil
			case []any:
				if len(v) > 0 {
					if s, ok := v[0].(string); ok {
						return s, nil
					}
				}
			}
		}
		return "", fmt.Errorf("unexpected clients5 response: %s", truncateStr(string(body), 160))
	}

	// gtx response: [[["translated", "original", ...], ...], ...]
	var root []any
	if err := json.Unmarshal(body, &root); err != nil || len(root) == 0 {
		return "", fmt.Errorf("unexpected gtx response: %s", truncateStr(string(body), 160))
	}
	sentences, ok := root[0].([]any)
	if !ok {
		return "", fmt.Errorf("unexpected gtx sentences: %s", truncateStr(string(body), 160))
	}
	var b strings.Builder
	for _, s := range sentences {
		pair, ok := s.([]any)
		if ok && len(pair) > 0 {
			if part, ok := pair[0].(string); ok {
				b.WriteString(part)
			}
		}
	}
	return b.String(), nil
}

func normalizeGoogleSourceLang(refLocale string) string {
	lower := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(refLocale), "_", "-"))
	switch lower {
	case "", "auto":
		return "auto"
	case "zh-cn", "zh-hans":
		return "zh-CN"
	case "zh-tw", "zh-hk", "zh-hant":
		return "zh-TW"
	default:
		if idx := strings.IndexByte(lower, '-'); idx > 0 {
			return lower[:idx]
		}
		return lower
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
