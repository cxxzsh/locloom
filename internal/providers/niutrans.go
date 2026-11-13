package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"locloom/internal/domain"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

const (
	defaultNiutransURL   = "https://api.niutrans.com/NiuTransServer/translation"
	niutransMaxBatchSize = 20
	niutransMaxBytes     = 4000
)

type niutransProvider struct {
	mu         sync.RWMutex
	apiKey     string
	sourceLang string
	baseURL    string
	useProxy   bool
	pool       *netpool.Pool
}

// NewNiutransProvider creates the built-in Niutrans translation provider.
func NewNiutransProvider() provider.Provider {
	return &niutransProvider{
		sourceLang: "auto",
		baseURL:    defaultNiutransURL,
	}
}

func (p *niutransProvider) ID() string   { return domain.OriginNiutrans }
func (p *niutransProvider) Name() string { return "小牛翻译 (NiuTrans)" }
func (p *niutransProvider) Description() string {
	return "调用小牛翻译开放平台 API 进行批量机器翻译"
}

func (p *niutransProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "NiuTrans API"
	}
	return p.Name()
}

func (p *niutransProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "Batch machine translation via NiuTrans Open Platform API"
	}
	return p.Description()
}

func (p *niutransProvider) Kind() provider.Kind { return provider.KindEngine }

func (p *niutransProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:         "api_key",
			Label:       "API Key",
			Type:        provider.FieldSecret,
			Placeholder: "小牛翻译 API Key",
		},
		{
			Key:     "source_lang",
			Label:   "源语言代码",
			Type:    provider.FieldText,
			Default: "auto",
		},
		{
			Key:     "base_url",
			Label:   "接口地址",
			Type:    provider.FieldText,
			Default: defaultNiutransURL,
		},
		{
			Key:     "use_proxy",
			Label:   "使用代理",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *niutransProvider) LocalizedConfigSchema(lang string) []provider.ConfigField {
	if lang != "en" {
		return p.ConfigSchema()
	}
	return []provider.ConfigField{
		{
			Key:         "api_key",
			Label:       "API Key",
			Type:        provider.FieldSecret,
			Placeholder: "NiuTrans API Key",
		},
		{
			Key:     "source_lang",
			Label:   "Source Language",
			Type:    provider.FieldText,
			Default: "auto",
		},
		{
			Key:     "base_url",
			Label:   "Base URL",
			Type:    provider.FieldText,
			Default: defaultNiutransURL,
		},
		{
			Key:     "use_proxy",
			Label:   "Use Proxy",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *niutransProvider) Configure(values map[string]string, pool *netpool.Pool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool = pool

	p.apiKey = strings.TrimSpace(values["api_key"])
	if sl := strings.TrimSpace(values["source_lang"]); sl != "" {
		p.sourceLang = sl
	} else {
		p.sourceLang = "auto"
	}
	if base := strings.TrimSpace(values["base_url"]); base != "" {
		p.baseURL = strings.TrimRight(base, "/")
	} else {
		p.baseURL = defaultNiutransURL
	}
	p.useProxy = values["use_proxy"] == "true"
	return nil
}

func (p *niutransProvider) getConfig() (apiKey, sourceLang, baseURL string, client *http.Client, err error) {
	p.mu.RLock()
	apiKey = p.apiKey
	sourceLang = p.sourceLang
	baseURL = p.baseURL
	useProxy := p.useProxy
	pool := p.pool
	p.mu.RUnlock()

	if apiKey == "" {
		return "", "", "", nil, fmt.Errorf("niutrans requires api_key")
	}
	if sourceLang == "" {
		sourceLang = "auto"
	}
	if baseURL == "" {
		baseURL = defaultNiutransURL
	}
	if pool == nil {
		pool = netpool.New(netpool.ProxyConfig{})
	}
	client, err = pool.Client(useProxy)
	if err != nil {
		return "", "", "", nil, err
	}
	return apiKey, sourceLang, baseURL, client, nil
}

func (p *niutransProvider) Check(ctx context.Context) error {
	apiKey, sourceLang, baseURL, client, err := p.getConfig()
	if err != nil {
		return err
	}
	res, err := callNiutransWithRetry(ctx, client, baseURL, apiKey, sourceLang, "zh", "Hello")
	if err != nil {
		return err
	}
	if res == "" {
		return fmt.Errorf("niutrans returned empty smoke check result")
	}
	return nil
}

func (p *niutransProvider) Translate(ctx context.Context, req provider.Request) (map[string]string, error) {
	if len(req.Items) == 0 {
		return map[string]string{}, nil
	}
	apiKey, sourceLang, baseURL, client, err := p.getConfig()
	if err != nil {
		return nil, err
	}

	tl := req.EngineCode
	if tl == "" {
		tl = req.TargetLocale.ID
	}

	out := make(map[string]string, len(req.Items))
	for start := 0; start < len(req.Items); {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		end := start
		byteCount := 0
		for end < len(req.Items) && (end-start) < niutransMaxBatchSize {
			txt := itemSourceText(req.Items[end])
			added := len(txt)
			if end > start {
				added++
			}
			if end > start && byteCount+added > niutransMaxBytes {
				break
			}
			byteCount += added
			end++
		}

		chunk := req.Items[start:end]
		texts := make([]string, len(chunk))
		for i, it := range chunk {
			texts[i] = itemSourceText(it)
		}

		translated, err := translateNiutransWithFallback(ctx, client, baseURL, apiKey, sourceLang, tl, texts)
		if err != nil {
			return out, err
		}
		for i, it := range chunk {
			if i < len(translated) && translated[i] != "" {
				out[it.ID] = strings.TrimSuffix(translated[i], "\r")
			}
		}
		start = end
	}
	return out, nil
}

func translateNiutransWithFallback(
	ctx context.Context,
	client *http.Client,
	baseURL, apiKey, sourceLang, targetLang string,
	items []string,
) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) == 1 {
		res, err := callNiutransWithRetry(ctx, client, baseURL, apiKey, sourceLang, targetLang, items[0])
		if err != nil {
			return nil, err
		}
		return []string{strings.TrimSuffix(res, "\r")}, nil
	}

	canBatch := true
	for _, s := range items {
		if strings.Contains(s, "\n") {
			canBatch = false
			break
		}
	}
	if canBatch {
		joined := strings.Join(items, "\n")
		if len(joined) <= niutransMaxBytes {
			res, err := callNiutransWithRetry(ctx, client, baseURL, apiKey, sourceLang, targetLang, joined)
			if err != nil {
				return nil, err
			}
			lines := strings.Split(res, "\n")
			if len(lines) == len(items) {
				for i := range lines {
					lines[i] = strings.TrimSuffix(lines[i], "\r")
				}
				return lines, nil
			}
		}
	}

	mid := len(items) / 2
	left, err := translateNiutransWithFallback(ctx, client, baseURL, apiKey, sourceLang, targetLang, items[:mid])
	if err != nil {
		return nil, err
	}
	right, err := translateNiutransWithFallback(ctx, client, baseURL, apiKey, sourceLang, targetLang, items[mid:])
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func isNiutransRateLimit(code, msg string) bool {
	if code == "13003" || code == "429" {
		return true
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "rate") ||
		strings.Contains(lower, "qps") ||
		strings.Contains(lower, "limit") ||
		strings.Contains(lower, "frequently")
}

func callNiutransWithRetry(
	ctx context.Context,
	client *http.Client,
	baseURL, apiKey, sourceLang, targetLang, srcText string,
) (string, error) {
	payload := map[string]string{
		"from":     sourceLang,
		"to":       targetLang,
		"apikey":   apiKey,
		"src_text": srcText,
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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(reqBody))
		if err != nil {
			return "", err
		}
		httpReq.Header.Set("Content-Type", "application/json")

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
			TgtText   string `json:"tgt_text"`
			ErrorCode string `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("invalid niutrans response JSON: %w", err)
		}
		if parsed.ErrorCode != "" || parsed.ErrorMsg != "" {
			if isNiutransRateLimit(parsed.ErrorCode, parsed.ErrorMsg) {
				lastErr = fmt.Errorf("niutrans rate limited (%s): %s", parsed.ErrorCode, parsed.ErrorMsg)
				continue
			}
			return "", fmt.Errorf("niutrans error %s: %s", parsed.ErrorCode, parsed.ErrorMsg)
		}
		return parsed.TgtText, nil
	}

	return "", lastErr
}
