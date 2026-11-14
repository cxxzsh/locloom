package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"locloom/internal/domain"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

const defaultLingvaBaseURL = "https://lingva.ml"

type libreTranslateProvider struct {
	mu        sync.RWMutex
	mode      string
	baseURL   string
	apiKey    string
	batchSize int
	useProxy  bool
	pool      *netpool.Pool
}

// NewLibreTranslateProvider creates the built-in Lingva / LibreTranslate provider.
func NewLibreTranslateProvider() provider.Provider {
	return &libreTranslateProvider{
		mode:      "lingva",
		baseURL:   defaultLingvaBaseURL,
		batchSize: 20,
	}
}

func (p *libreTranslateProvider) ID() string   { return domain.OriginLibre }
func (p *libreTranslateProvider) Name() string { return "Lingva / LibreTranslate 开源翻译" }
func (p *libreTranslateProvider) Description() string {
	return "调用 Lingva 免 Key 接口或自托管 LibreTranslate 实例进行翻译"
}

func (p *libreTranslateProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "Lingva / LibreTranslate"
	}
	return p.Name()
}

func (p *libreTranslateProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "Translate via public Lingva or self-hosted LibreTranslate instance"
	}
	return p.Description()
}

func (p *libreTranslateProvider) Kind() provider.Kind { return provider.KindEngine }

func (p *libreTranslateProvider) CacheNamespace() string {
	p.mu.RLock()
	mode := p.mode
	baseURL := p.baseURL
	p.mu.RUnlock()
	return mode + "|" + strings.TrimRight(baseURL, "/")
}

func (p *libreTranslateProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:     "mode",
			Label:   "接口模式",
			Type:    provider.FieldSelect,
			Default: "lingva",
			Options: []string{"lingva", "libretranslate"},
		},
		{
			Key:         "base_url",
			Label:       "服务基础地址",
			Type:        provider.FieldText,
			Default:     defaultLingvaBaseURL,
			Placeholder: "http://127.0.0.1:5000",
			Help:        "Lingva 模式默认使用 https://lingva.ml，LibreTranslate 模式通常为 http://127.0.0.1:5000",
		},
		{
			Key:   "api_key",
			Label: "API Key（LibreTranslate 可选）",
			Type:  provider.FieldSecret,
		},
		{
			Key:     "batch_size",
			Label:   "单批词条数",
			Type:    provider.FieldText,
			Default: "20",
		},
		{
			Key:     "use_proxy",
			Label:   "使用代理",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *libreTranslateProvider) LocalizedConfigSchema(lang string) []provider.ConfigField {
	if lang != "en" {
		return p.ConfigSchema()
	}
	return []provider.ConfigField{
		{
			Key:     "mode",
			Label:   "Mode",
			Type:    provider.FieldSelect,
			Default: "lingva",
			Options: []string{"lingva", "libretranslate"},
		},
		{
			Key:         "base_url",
			Label:       "Base URL",
			Type:        provider.FieldText,
			Default:     defaultLingvaBaseURL,
			Placeholder: "http://127.0.0.1:5000",
			Help:        "Use e.g. https://lingva.ml for Lingva or http://127.0.0.1:5000 for LibreTranslate",
		},
		{
			Key:   "api_key",
			Label: "API Key (optional for LibreTranslate)",
			Type:  provider.FieldSecret,
		},
		{
			Key:     "batch_size",
			Label:   "Batch Size",
			Type:    provider.FieldText,
			Default: "20",
		},
		{
			Key:     "use_proxy",
			Label:   "Use Proxy",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *libreTranslateProvider) Configure(values map[string]string, pool *netpool.Pool) error {
	mode := strings.TrimSpace(values["mode"])
	if mode != "libretranslate" {
		mode = "lingva"
	}
	baseURL := strings.TrimRight(strings.TrimSpace(values["base_url"]), "/")
	if baseURL == "" {
		baseURL = defaultLingvaBaseURL
	}
	batchSize := 20
	if rawBS := strings.TrimSpace(values["batch_size"]); rawBS != "" {
		if bs, err := strconv.Atoi(rawBS); err == nil {
			if bs < 1 {
				bs = 1
			} else if bs > 50 {
				bs = 50
			}
			batchSize = bs
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool = pool
	p.mode = mode
	p.baseURL = baseURL
	p.apiKey = strings.TrimSpace(values["api_key"])
	p.batchSize = batchSize
	p.useProxy = values["use_proxy"] == "true"
	return nil
}

func (p *libreTranslateProvider) snapshot() (mode, baseURL, apiKey string, batchSize int, client *http.Client, err error) {
	p.mu.RLock()
	pool := p.pool
	mode = p.mode
	baseURL = p.baseURL
	apiKey = p.apiKey
	batchSize = p.batchSize
	useProxy := p.useProxy
	p.mu.RUnlock()

	if pool == nil {
		pool = netpool.New(netpool.ProxyConfig{})
	}
	client, err = pool.Client(useProxy)
	if err != nil {
		return "", "", "", 0, nil, err
	}
	if mode == "" {
		mode = "lingva"
	}
	if baseURL == "" {
		baseURL = defaultLingvaBaseURL
	}
	if batchSize <= 0 {
		batchSize = 20
	}
	return mode, baseURL, apiKey, batchSize, client, nil
}

func (p *libreTranslateProvider) Check(ctx context.Context) error {
	mode, baseURL, apiKey, _, client, err := p.snapshot()
	if err != nil {
		return err
	}
	if mode == "libretranslate" {
		res, err := callLibreTranslateBatch(ctx, client, baseURL, apiKey, "zh", []string{"Hello"})
		if err != nil {
			return err
		}
		if len(res) == 0 || res[0] == "" {
			return fmt.Errorf("libretranslate returned empty smoke check result")
		}
		return nil
	}
	res, err := callLingva(ctx, client, baseURL, "zh", "Hello")
	if err != nil {
		return err
	}
	if res == "" {
		return fmt.Errorf("lingva returned empty smoke check result")
	}
	return nil
}

func (p *libreTranslateProvider) Translate(ctx context.Context, req provider.Request) (map[string]string, error) {
	if len(req.Items) == 0 {
		return map[string]string{}, nil
	}
	mode, baseURL, apiKey, batchSize, client, err := p.snapshot()
	if err != nil {
		return nil, err
	}

	tl := req.EngineCode
	if tl == "" {
		tl = req.TargetLocale.ID
	}

	out := make(map[string]string, len(req.Items))
	if mode == "lingva" {
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
			translated, err := callLingva(ctx, client, baseURL, tl, text)
			if err != nil {
				return out, err
			}
			if translated != "" {
				out[it.ID] = translated
			}
		}
		return out, nil
	}

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

		translated, err := callLibreTranslateBatch(ctx, client, baseURL, apiKey, tl, texts)
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

func mapLingvaTargetLang(tl string) string {
	if strings.EqualFold(strings.TrimSpace(tl), "zt") {
		return "zh_HANT"
	}
	return tl
}

func callLingva(ctx context.Context, client *http.Client, baseURL, tl, text string) (string, error) {
	target := mapLingvaTargetLang(tl)
	reqURL := fmt.Sprintf(
		"%s/api/v1/auto/%s/%s",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(target),
		url.PathEscape(text),
	)

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
			Translation string `json:"translation"`
			Error       string `json:"error"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("invalid lingva response JSON: %w", err)
		}
		if parsed.Error != "" {
			return "", fmt.Errorf("lingva error: %s", parsed.Error)
		}
		return strings.TrimSuffix(parsed.Translation, "\r"), nil
	}
	return "", lastErr
}

func callLibreTranslateBatch(
	ctx context.Context,
	client *http.Client,
	baseURL, apiKey, tl string,
	texts []string,
) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := struct {
		Q      []string `json:"q"`
		Source string   `json:"source"`
		Target string   `json:"target"`
		Format string   `json:"format"`
		APIKey string   `json:"api_key,omitempty"`
	}{
		Q:      texts,
		Source: "auto",
		Target: tl,
		Format: "text",
		APIKey: apiKey,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/translate"
	body, statusCode, err := postJSONWithRetry(ctx, client, endpoint, payload)
	if err != nil {
		return nil, err
	}

	// If the server rejects array payloads with 400/422, fall back to item-by-item requests.
	if statusCode == http.StatusBadRequest || statusCode == http.StatusUnprocessableEntity {
		if len(texts) > 1 {
			return callLibreTranslateItemByItem(ctx, client, endpoint, apiKey, tl, texts)
		}
		// Even for a single item, retry with string "q" in case the server rejects []string.
		single, serr := callLibreTranslateSingle(ctx, client, endpoint, apiKey, tl, texts[0])
		if serr == nil {
			return []string{single}, nil
		}
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, truncateStr(string(body), 200))
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, truncateStr(string(body), 200))
	}

	var rawResp struct {
		TranslatedText json.RawMessage `json:"translatedText"`
		Error          string          `json:"error"`
	}
	if err := json.Unmarshal(body, &rawResp); err != nil {
		return nil, fmt.Errorf("invalid libretranslate response JSON: %w", err)
	}
	if rawResp.Error != "" {
		return nil, fmt.Errorf("libretranslate error: %s", rawResp.Error)
	}

	var batchOut []string
	if err := json.Unmarshal(rawResp.TranslatedText, &batchOut); err == nil {
		if len(batchOut) == len(texts) {
			for i := range batchOut {
				batchOut[i] = strings.TrimSuffix(batchOut[i], "\r")
			}
			return batchOut, nil
		}
	}

	var singleOut string
	if err := json.Unmarshal(rawResp.TranslatedText, &singleOut); err == nil {
		if len(texts) == 1 {
			return []string{strings.TrimSuffix(singleOut, "\r")}, nil
		}
		return callLibreTranslateItemByItem(ctx, client, endpoint, apiKey, tl, texts)
	}

	return nil, fmt.Errorf("unexpected libretranslate translatedText: %s", truncateStr(string(rawResp.TranslatedText), 160))
}

func callLibreTranslateItemByItem(
	ctx context.Context,
	client *http.Client,
	endpoint, apiKey, tl string,
	texts []string,
) ([]string, error) {
	out := make([]string, len(texts))
	for i, text := range texts {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		res, err := callLibreTranslateSingle(ctx, client, endpoint, apiKey, tl, text)
		if err != nil {
			return nil, err
		}
		out[i] = res
	}
	return out, nil
}

func callLibreTranslateSingle(
	ctx context.Context,
	client *http.Client,
	endpoint, apiKey, tl, text string,
) (string, error) {
	reqBody := struct {
		Q      string `json:"q"`
		Source string `json:"source"`
		Target string `json:"target"`
		Format string `json:"format"`
		APIKey string `json:"api_key,omitempty"`
	}{
		Q:      text,
		Source: "auto",
		Target: tl,
		Format: "text",
		APIKey: apiKey,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	body, statusCode, err := postJSONWithRetry(ctx, client, endpoint, payload)
	if err != nil {
		return "", err
	}
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", statusCode, truncateStr(string(body), 200))
	}

	var resp struct {
		TranslatedText string `json:"translatedText"`
		Error          string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("invalid libretranslate response JSON: %w", err)
	}
	if resp.Error != "" {
		return "", fmt.Errorf("libretranslate error: %s", resp.Error)
	}
	return strings.TrimSuffix(resp.TranslatedText, "\r"), nil
}

func postJSONWithRetry(ctx context.Context, client *http.Client, endpoint string, payload []byte) ([]byte, int, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			backoff := time.Duration(500*(1<<(attempt-1))) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, 0, err
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
		return body, resp.StatusCode, nil
	}
	return nil, 0, lastErr
}
