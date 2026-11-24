package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"locloom/internal/domain"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

const defaultMyMemoryBaseURL = "https://api.mymemory.translated.net"

type myMemoryProvider struct {
	mu         sync.RWMutex
	baseURL    string
	sourceLang string
	email      string
	apiKey     string
	useProxy   bool
	pool       *netpool.Pool
}

// NewMyMemoryProvider creates the built-in MyMemory machine translation provider.
func NewMyMemoryProvider() provider.Provider {
	return &myMemoryProvider{
		baseURL:    defaultMyMemoryBaseURL,
		sourceLang: "en",
	}
}

func (p *myMemoryProvider) ID() string   { return domain.OriginMyMemory }
func (p *myMemoryProvider) Name() string { return "MyMemory 翻译" }
func (p *myMemoryProvider) Description() string {
	return "调用 MyMemory 公共翻译记忆库与机器翻译 API"
}

func (p *myMemoryProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "MyMemory Translate"
	}
	return p.Name()
}

func (p *myMemoryProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "Translate via MyMemory public translation memory and MT API"
	}
	return p.Description()
}

func (p *myMemoryProvider) Kind() provider.Kind { return provider.KindEngine }

func (p *myMemoryProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:     "base_url",
			Label:   "API 基础地址",
			Type:    provider.FieldText,
			Default: defaultMyMemoryBaseURL,
		},
		{
			Key:     "source_lang",
			Label:   "源语言代码",
			Type:    provider.FieldText,
			Default: "en",
			Help:    "MyMemory 需要在 langpair 参数中显式指定源语言（例如 en）",
		},
		{
			Key:         "email",
			Label:       "联系邮箱（可选）",
			Type:        provider.FieldText,
			Placeholder: "you@example.com",
			Help:        "提供联系邮箱将作为 de 参数发送，可将免费额度提升约 10 倍",
		},
		{
			Key:   "api_key",
			Label: "API Key（可选）",
			Type:  provider.FieldSecret,
		},
		{
			Key:     "use_proxy",
			Label:   "使用代理",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *myMemoryProvider) LocalizedConfigSchema(lang string) []provider.ConfigField {
	if lang != "en" {
		return p.ConfigSchema()
	}
	return []provider.ConfigField{
		{
			Key:     "base_url",
			Label:   "Base URL",
			Type:    provider.FieldText,
			Default: defaultMyMemoryBaseURL,
		},
		{
			Key:     "source_lang",
			Label:   "Source Language",
			Type:    provider.FieldText,
			Default: "en",
			Help:    "MyMemory requires an explicit source language in langpair (e.g. en)",
		},
		{
			Key:         "email",
			Label:       "Contact Email (optional)",
			Type:        provider.FieldText,
			Placeholder: "you@example.com",
			Help:        "Sent as &de=<email> to increase the free daily quota by 10x",
		},
		{
			Key:   "api_key",
			Label: "API Key (optional)",
			Type:  provider.FieldSecret,
		},
		{
			Key:     "use_proxy",
			Label:   "Use Proxy",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *myMemoryProvider) Configure(values map[string]string, pool *netpool.Pool) error {
	baseURL := strings.TrimRight(strings.TrimSpace(values["base_url"]), "/")
	if baseURL == "" {
		baseURL = defaultMyMemoryBaseURL
	}
	sourceLang := strings.TrimSpace(values["source_lang"])
	if sourceLang == "" {
		sourceLang = "en"
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool = pool
	p.baseURL = baseURL
	p.sourceLang = sourceLang
	p.email = strings.TrimSpace(values["email"])
	p.apiKey = strings.TrimSpace(values["api_key"])
	p.useProxy = values["use_proxy"] == "true"
	return nil
}

func (p *myMemoryProvider) snapshot() (baseURL, sourceLang, email, apiKey string, client *http.Client, err error) {
	p.mu.RLock()
	pool := p.pool
	baseURL = p.baseURL
	sourceLang = p.sourceLang
	email = p.email
	apiKey = p.apiKey
	useProxy := p.useProxy
	p.mu.RUnlock()

	if pool == nil {
		pool = netpool.New(netpool.ProxyConfig{})
	}
	client, err = pool.Client(useProxy)
	if err != nil {
		return "", "", "", "", nil, err
	}
	if baseURL == "" {
		baseURL = defaultMyMemoryBaseURL
	}
	if sourceLang == "" {
		sourceLang = "en"
	}
	return baseURL, sourceLang, email, apiKey, client, nil
}

func (p *myMemoryProvider) Check(ctx context.Context) error {
	baseURL, _, email, apiKey, client, err := p.snapshot()
	if err != nil {
		return err
	}
	res, err := callMyMemory(ctx, client, baseURL, "en", "zh-CN", email, apiKey, "Hello")
	if err != nil {
		return err
	}
	if res == "" {
		return fmt.Errorf("mymemory returned empty smoke check result")
	}
	return nil
}

func (p *myMemoryProvider) Translate(ctx context.Context, req provider.Request) (map[string]string, error) {
	if len(req.Items) == 0 {
		return map[string]string{}, nil
	}
	baseURL, sourceLang, email, apiKey, client, err := p.snapshot()
	if err != nil {
		return nil, err
	}

	tl := req.EngineCode
	if tl == "" {
		tl = req.TargetLocale.ID
	}

	out := make(map[string]string, len(req.Items))
	for _, it := range req.Items {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		text := it.Key.Source
		if text == "" {
			text = it.RefText
		}
		if text == "" {
			continue
		}
		itemSrcLang := sourceLang
		if it.SourceLocale != "" {
			norm := strings.ReplaceAll(strings.TrimSpace(it.SourceLocale), "_", "-")
			if parts := strings.SplitN(norm, "-", 2); len(parts) == 2 {
				itemSrcLang = strings.ToLower(parts[0]) + "-" + strings.ToUpper(parts[1])
			} else if norm != "" {
				itemSrcLang = strings.ToLower(norm)
			}
		}
		if strings.EqualFold(itemSrcLang, tl) {
			out[it.ID] = text
			continue
		}

		translated, err := callMyMemory(ctx, client, baseURL, itemSrcLang, tl, email, apiKey, text)
		if err != nil {
			return out, err
		}
		if translated != "" {
			out[it.ID] = translated
		}
	}
	return out, nil
}

func callMyMemory(
	ctx context.Context,
	client *http.Client,
	baseURL, src, tl, email, apiKey, text string,
) (string, error) {
	reqURL := fmt.Sprintf(
		"%s/get?q=%s&langpair=%s",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(text),
		url.QueryEscape(src+"|"+tl),
	)
	if email != "" {
		reqURL += "&de=" + url.QueryEscape(email)
	}
	if apiKey != "" {
		reqURL += "&key=" + url.QueryEscape(apiKey)
	}

	const maxAttempts = 3
	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			backoff := time.Duration(500*(1<<(attempt-1))) * time.Millisecond
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", err
		}
		httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; locloom/2.0)")

		resp, err := client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}

		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}

		if isRetryableStatus(resp.StatusCode) {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 160))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
		}

		var parsed struct {
			ResponseData struct {
				TranslatedText string `json:"translatedText"`
			} `json:"responseData"`
			ResponseStatus  int    `json:"responseStatus"`
			ResponseDetails string `json:"responseDetails"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("invalid mymemory response JSON: %w", err)
		}
		if parsed.ResponseStatus != 200 && parsed.ResponseStatus != 0 {
			return "", fmt.Errorf("mymemory error status %d: %s", parsed.ResponseStatus, parsed.ResponseDetails)
		}

		translated := strings.TrimSuffix(parsed.ResponseData.TranslatedText, "\r")
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(translated)), "MYMEMORY WARNING:") {
			return "", fmt.Errorf("mymemory quota warning: %s", strings.TrimSpace(translated))
		}
		return translated, nil
	}

	return "", lastErr
}
