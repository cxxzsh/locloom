package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"locloom/internal/domain"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

const (
	defaultDeepLXURL      = "http://127.0.0.1:1188/translate"
	defaultDeepLFreeURL   = "https://api-free.deepl.com/v2/translate"
	defaultDeepLPaidURL   = "https://api.deepl.com/v2/translate"
	defaultDeepLBatchSize = 25
)

type deepLProvider struct {
	mu        sync.RWMutex
	mode      string
	baseURL   string
	apiKey    string
	batchSize int
	useProxy  bool
	pool      *netpool.Pool
}

// NewDeepLProvider creates the built-in DeepL / DeepLX translation provider.
func NewDeepLProvider() provider.Provider {
	return &deepLProvider{
		mode:      "deeplx",
		baseURL:   defaultDeepLXURL,
		batchSize: defaultDeepLBatchSize,
	}
}

func (p *deepLProvider) ID() string   { return domain.OriginDeepL }
func (p *deepLProvider) Name() string { return "DeepL / DeepLX 翻译" }
func (p *deepLProvider) Description() string {
	return "支持 DeepL 官方 API（Free / Pro 批量翻译）与本地/自建 DeepLX 接口"
}

func (p *deepLProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "DeepL / DeepLX Translate"
	}
	return p.Name()
}

func (p *deepLProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "Translate via DeepL Official API (Free / Pro batching) or self-hosted DeepLX endpoint"
	}
	return p.Description()
}

func (p *deepLProvider) Kind() provider.Kind { return provider.KindEngine }

func (p *deepLProvider) CacheNamespace() string {
	p.mu.RLock()
	mode := p.mode
	baseURL := p.baseURL
	apiKey := p.apiKey
	p.mu.RUnlock()
	if ep, err := resolveDeepLEndpoint(mode, baseURL, apiKey); err == nil {
		return mode + "|" + ep
	}
	return mode + "|" + strings.TrimRight(baseURL, "/")
}

func (p *deepLProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:     "mode",
			Label:   "接口模式",
			Type:    provider.FieldSelect,
			Default: "deeplx",
			Options: []string{"deeplx", "official"},
		},
		{
			Key:         "base_url",
			Label:       "接口地址",
			Type:        provider.FieldText,
			Default:     defaultDeepLXURL,
			Placeholder: defaultDeepLXURL,
			Help:        "DeepLX /translate 地址或自定义 DeepL 官方接口地址；official 模式下若留空或仍为 127.0.0.1:1188 将按 API Key 自动选择官方端点",
		},
		{
			Key:         "api_key",
			Label:       "API Key / Token",
			Type:        provider.FieldSecret,
			Placeholder: "official 模式必填；deeplx 模式可选 Bearer Token",
		},
		{
			Key:     "batch_size",
			Label:   "单批词条数",
			Type:    provider.FieldText,
			Default: "25",
			Help:    "official 模式每批词条数（1-50）",
		},
		{
			Key:     "use_proxy",
			Label:   "使用代理",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *deepLProvider) LocalizedConfigSchema(lang string) []provider.ConfigField {
	if lang != "en" {
		return p.ConfigSchema()
	}
	return []provider.ConfigField{
		{
			Key:     "mode",
			Label:   "API Mode",
			Type:    provider.FieldSelect,
			Default: "deeplx",
			Options: []string{"deeplx", "official"},
		},
		{
			Key:         "base_url",
			Label:       "Base URL",
			Type:        provider.FieldText,
			Default:     defaultDeepLXURL,
			Placeholder: defaultDeepLXURL,
			Help:        "DeepLX /translate URL or custom official DeepL URL; in official mode, empty or 127.0.0.1:1188 auto-selects the official DeepL endpoint by key",
		},
		{
			Key:         "api_key",
			Label:       "API Key / Token",
			Type:        provider.FieldSecret,
			Placeholder: "Required in official mode; optional Bearer token in deeplx mode",
		},
		{
			Key:     "batch_size",
			Label:   "Batch Size",
			Type:    provider.FieldText,
			Default: "25",
			Help:    "Items per request in official mode (1-50)",
		},
		{
			Key:     "use_proxy",
			Label:   "Use Proxy",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *deepLProvider) Configure(values map[string]string, pool *netpool.Pool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool = pool

	mode := strings.ToLower(strings.TrimSpace(values["mode"]))
	if mode != "official" {
		mode = "deeplx"
	}
	p.mode = mode

	baseURL := strings.TrimSpace(values["base_url"])
	if baseURL == "" {
		baseURL = defaultDeepLXURL
	}
	p.baseURL = baseURL
	p.apiKey = strings.TrimSpace(values["api_key"])
	p.useProxy = values["use_proxy"] == "true"

	if bs, err := strconv.Atoi(strings.TrimSpace(values["batch_size"])); err == nil {
		if bs < 1 {
			bs = 1
		} else if bs > 50 {
			bs = 50
		}
		p.batchSize = bs
	} else {
		p.batchSize = defaultDeepLBatchSize
	}
	return nil
}

func resolveDeepLEndpoint(mode, baseURL, apiKey string) (string, error) {
	if mode == "official" {
		if apiKey == "" {
			return "", fmt.Errorf("deepl official mode requires api_key")
		}
		if baseURL == "" || strings.Contains(baseURL, "127.0.0.1:1188") {
			if strings.HasSuffix(apiKey, ":fx") {
				return defaultDeepLFreeURL, nil
			}
			return defaultDeepLPaidURL, nil
		}
		return strings.TrimRight(baseURL, "/"), nil
	}

	if baseURL == "" {
		return defaultDeepLXURL, nil
	}
	return strings.TrimRight(baseURL, "/"), nil
}

func (p *deepLProvider) getConfig() (mode, endpoint, apiKey string, batchSize int, client *http.Client, err error) {
	p.mu.RLock()
	mode = p.mode
	baseURL := p.baseURL
	apiKey = p.apiKey
	batchSize = p.batchSize
	useProxy := p.useProxy
	pool := p.pool
	p.mu.RUnlock()

	endpoint, err = resolveDeepLEndpoint(mode, baseURL, apiKey)
	if err != nil {
		return "", "", "", 0, nil, err
	}

	if pool == nil {
		pool = netpool.New(netpool.ProxyConfig{})
	}
	client, err = pool.Client(useProxy)
	if err != nil {
		return "", "", "", 0, nil, err
	}
	return mode, endpoint, apiKey, batchSize, client, nil
}

func (p *deepLProvider) Check(ctx context.Context) error {
	mode, endpoint, apiKey, _, client, err := p.getConfig()
	if err != nil {
		return err
	}
	if mode == "official" {
		res, err := callDeepLOfficialWithRetry(ctx, client, endpoint, apiKey, "ZH", []string{"Hello, [[0]]!"})
		if err != nil {
			return err
		}
		if len(res) == 0 || res[0] == "" {
			return fmt.Errorf("deepl official returned empty smoke check result")
		}
		return nil
	}

	res, err := callDeepLXWithRetry(ctx, client, endpoint, apiKey, "ZH", "Hello, [[0]]!")
	if err != nil {
		return err
	}
	if res == "" {
		return fmt.Errorf("deeplx returned empty smoke check result")
	}
	return nil
}

func (p *deepLProvider) Translate(ctx context.Context, req provider.Request) (map[string]string, error) {
	if len(req.Items) == 0 {
		return map[string]string{}, nil
	}
	mode, endpoint, apiKey, batchSize, client, err := p.getConfig()
	if err != nil {
		return nil, err
	}

	tl := req.EngineCode
	if tl == "" {
		tl = req.TargetLocale.ID
	}
	targetLang := strings.ToUpper(strings.TrimSpace(tl))

	out := make(map[string]string, len(req.Items))
	if mode == "official" {
		for start := 0; start < len(req.Items); start += batchSize {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			end := min(start+batchSize, len(req.Items))
			chunk := req.Items[start:end]

			texts := make([]string, len(chunk))
			for i, it := range chunk {
				texts[i] = itemSourceText(it)
			}

			translated, err := callDeepLOfficialWithRetry(ctx, client, endpoint, apiKey, targetLang, texts)
			if err != nil {
				return out, err
			}
			for i, it := range chunk {
				if i < len(translated) && translated[i] != "" {
					out[it.ID] = strings.TrimSuffix(translated[i], "\r")
				}
			}
		}
		return out, nil
	}

	// DeepLX translates one string per request.
	for _, it := range req.Items {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		translated, err := callDeepLXWithRetry(ctx, client, endpoint, apiKey, targetLang, itemSourceText(it))
		if err != nil {
			return out, err
		}
		if translated != "" {
			out[it.ID] = strings.TrimSuffix(translated, "\r")
		}
	}
	return out, nil
}

func itemSourceText(it provider.SourceItem) string {
	if it.Key.Source != "" {
		return it.Key.Source
	}
	return it.RefText
}

func callDeepLOfficialWithRetry(
	ctx context.Context,
	client *http.Client,
	endpoint, apiKey, targetLang string,
	texts []string,
) ([]string, error) {
	payload := map[string]any{
		"text":        texts,
		"target_lang": targetLang,
	}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	const maxAttempts = 4
	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			backoff := time.Duration(500*(1<<(attempt-1))) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "DeepL-Auth-Key "+apiKey)

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
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
		}

		var parsed struct {
			Translations []struct {
				Text string `json:"text"`
			} `json:"translations"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("invalid deepl response JSON: %w", err)
		}
		if len(parsed.Translations) != len(texts) {
			return nil, fmt.Errorf("deepl returned %d translations for %d items", len(parsed.Translations), len(texts))
		}
		res := make([]string, len(parsed.Translations))
		for i, tr := range parsed.Translations {
			res[i] = tr.Text
		}
		return res, nil
	}

	return nil, lastErr
}

func callDeepLXWithRetry(
	ctx context.Context,
	client *http.Client,
	endpoint, apiKey, targetLang, text string,
) (string, error) {
	payload := map[string]any{
		"text":        text,
		"source_lang": "auto",
		"target_lang": targetLang,
	}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	const maxAttempts = 4
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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
		if err != nil {
			return "", err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

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
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    string `json:"data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("invalid deeplx response JSON: %w", err)
		}
		if parsed.Code == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("deeplx rate limited (429): %s", parsed.Message)
			continue
		}
		if parsed.Code != 0 && parsed.Code != http.StatusOK {
			return "", fmt.Errorf("deeplx error %d: %s", parsed.Code, parsed.Message)
		}
		return parsed.Data, nil
	}

	return "", lastErr
}
