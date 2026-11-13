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

const (
	defaultBingEdgeEndpoint  = "https://edge.microsoft.com/translate/translatetext"
	defaultBingAzureEndpoint = "https://api.cognitive.microsofttranslator.com/translate"
	bingEdgeUserAgent        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"
)

type bingWebProvider struct {
	mu          sync.RWMutex
	mode        string
	endpointURL string
	apiKey      string
	region      string
	batchSize   int
	useProxy    bool
	pool        *netpool.Pool
}

// NewBingWebProvider creates the built-in Bing / Azure Translator provider.
func NewBingWebProvider() provider.Provider {
	return &bingWebProvider{
		mode:        "edge_free",
		endpointURL: defaultBingEdgeEndpoint,
		batchSize:   25,
	}
}

func (p *bingWebProvider) ID() string   { return domain.OriginBing }
func (p *bingWebProvider) Name() string { return "Bing / Azure 翻译" }
func (p *bingWebProvider) Description() string {
	return "调用 Edge 免费接口或 Azure Translator v3 接口批量翻译"
}

func (p *bingWebProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "Bing / Azure Translator"
	}
	return p.Name()
}

func (p *bingWebProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "Batch translate via free Edge Translator or Azure Translator v3 API"
	}
	return p.Description()
}

func (p *bingWebProvider) Kind() provider.Kind { return provider.KindEngine }

func (p *bingWebProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:     "mode",
			Label:   "接口模式",
			Type:    provider.FieldSelect,
			Default: "edge_free",
			Options: []string{"edge_free", "azure_key"},
		},
		{
			Key:         "endpoint_url",
			Label:       "接口地址",
			Type:        provider.FieldText,
			Default:     defaultBingEdgeEndpoint,
			Placeholder: defaultBingAzureEndpoint,
		},
		{
			Key:   "api_key",
			Label: "API Key（Azure 模式必填）",
			Type:  provider.FieldSecret,
		},
		{
			Key:         "region",
			Label:       "Azure 区域（可选）",
			Type:        provider.FieldText,
			Placeholder: "eastasia",
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

func (p *bingWebProvider) LocalizedConfigSchema(lang string) []provider.ConfigField {
	if lang != "en" {
		return p.ConfigSchema()
	}
	return []provider.ConfigField{
		{
			Key:     "mode",
			Label:   "Mode",
			Type:    provider.FieldSelect,
			Default: "edge_free",
			Options: []string{"edge_free", "azure_key"},
		},
		{
			Key:         "endpoint_url",
			Label:       "Endpoint URL",
			Type:        provider.FieldText,
			Default:     defaultBingEdgeEndpoint,
			Placeholder: defaultBingAzureEndpoint,
		},
		{
			Key:   "api_key",
			Label: "API Key (required for azure_key)",
			Type:  provider.FieldSecret,
		},
		{
			Key:         "region",
			Label:       "Azure Region (optional)",
			Type:        provider.FieldText,
			Placeholder: "eastasia",
		},
		{
			Key:     "batch_size",
			Label:   "Batch Size",
			Type:    provider.FieldText,
			Default: "25",
		},
		{
			Key:     "use_proxy",
			Label:   "Use Proxy",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *bingWebProvider) Configure(values map[string]string, pool *netpool.Pool) error {
	mode := strings.TrimSpace(values["mode"])
	if mode != "azure_key" {
		mode = "edge_free"
	}
	apiKey := strings.TrimSpace(values["api_key"])
	if mode == "azure_key" && apiKey == "" {
		return fmt.Errorf("bing_web: api_key is required when mode is azure_key")
	}

	endpointURL := strings.TrimRight(strings.TrimSpace(values["endpoint_url"]), "/")
	if endpointURL == "" {
		endpointURL = defaultBingEdgeEndpoint
	}

	region := strings.TrimSpace(values["region"])
	batchSize := 25
	if rawBS := strings.TrimSpace(values["batch_size"]); rawBS != "" {
		if bs, err := strconv.Atoi(rawBS); err == nil {
			if bs < 1 {
				bs = 1
			} else if bs > 100 {
				bs = 100
			}
			batchSize = bs
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool = pool
	p.mode = mode
	p.endpointURL = endpointURL
	p.apiKey = apiKey
	p.region = region
	p.batchSize = batchSize
	p.useProxy = values["use_proxy"] == "true"
	return nil
}

func (p *bingWebProvider) snapshot() (mode, endpointURL, apiKey, region string, batchSize int, client *http.Client, err error) {
	p.mu.RLock()
	pool := p.pool
	mode = p.mode
	endpointURL = p.endpointURL
	apiKey = p.apiKey
	region = p.region
	batchSize = p.batchSize
	useProxy := p.useProxy
	p.mu.RUnlock()

	if pool == nil {
		pool = netpool.New(netpool.ProxyConfig{})
	}
	client, err = pool.Client(useProxy)
	if err != nil {
		return "", "", "", "", 0, nil, err
	}
	if mode == "" {
		mode = "edge_free"
	}
	if batchSize <= 0 {
		batchSize = 25
	}
	return mode, endpointURL, apiKey, region, batchSize, client, nil
}

func (p *bingWebProvider) Check(ctx context.Context) error {
	mode, endpointURL, apiKey, region, _, client, err := p.snapshot()
	if err != nil {
		return err
	}
	res, err := callBingTranslate(ctx, client, mode, endpointURL, apiKey, region, "zh-Hans", []string{"Hello, [[0]]!"})
	if err != nil {
		return err
	}
	if len(res) == 0 || res[0] == "" {
		return fmt.Errorf("bing_web returned empty smoke check result")
	}
	return nil
}

func (p *bingWebProvider) Translate(ctx context.Context, req provider.Request) (map[string]string, error) {
	if len(req.Items) == 0 {
		return map[string]string{}, nil
	}
	mode, endpointURL, apiKey, region, batchSize, client, err := p.snapshot()
	if err != nil {
		return nil, err
	}

	tl := req.EngineCode
	if tl == "" {
		tl = req.TargetLocale.ID
	}

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

		translated, err := callBingTranslate(ctx, client, mode, endpointURL, apiKey, region, tl, texts)
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

func callBingTranslate(
	ctx context.Context,
	client *http.Client,
	mode, endpointURL, apiKey, region, tl string,
	texts []string,
) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	var reqURL string
	var payload []byte
	var err error

	if mode == "azure_key" {
		if apiKey == "" {
			return nil, fmt.Errorf("bing_web: api_key is required in azure_key mode")
		}
		base := strings.TrimRight(strings.TrimSpace(endpointURL), "/")
		if base == "" || base == defaultBingEdgeEndpoint {
			base = defaultBingAzureEndpoint
		}
		reqURL = fmt.Sprintf("%s?api-version=3.0&to=%s", base, url.QueryEscape(tl))
		bodyItems := make([]struct {
			Text string `json:"Text"`
		}, len(texts))
		for i, t := range texts {
			bodyItems[i].Text = t
		}
		payload, err = json.Marshal(bodyItems)
	} else {
		base := strings.TrimRight(strings.TrimSpace(endpointURL), "/")
		if base == "" {
			base = defaultBingEdgeEndpoint
		}
		reqURL = fmt.Sprintf("%s?from=&to=%s&isEnterpriseClient=false", base, url.QueryEscape(tl))
		payload, err = json.Marshal(texts)
	}
	if err != nil {
		return nil, err
	}

	const maxAttempts = 3
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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if mode == "azure_key" {
			httpReq.Header.Set("Ocp-Apim-Subscription-Key", apiKey)
			if region != "" {
				httpReq.Header.Set("Ocp-Apim-Subscription-Region", region)
			}
		} else {
			httpReq.Header.Set("User-Agent", bingEdgeUserAgent)
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
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
		}

		var parsed []struct {
			Translations []struct {
				Text string `json:"text"`
			} `json:"translations"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("invalid bing response JSON: %w", err)
		}
		if len(parsed) != len(texts) {
			return nil, fmt.Errorf("bing response count mismatch: got %d, want %d", len(parsed), len(texts))
		}

		out := make([]string, len(texts))
		for i, item := range parsed {
			if len(item.Translations) > 0 {
				out[i] = strings.TrimSuffix(item.Translations[0].Text, "\r")
			}
		}
		return out, nil
	}

	return nil, lastErr
}
