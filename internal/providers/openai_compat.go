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

type openAICompatProvider struct {
	mu           sync.RWMutex
	useProxy     bool
	baseURL      string
	model        string
	apiKey       string
	domainPrompt string
	pool         *netpool.Pool
}

// NewOpenAICompatProvider creates the built-in OpenAI-compatible LLM translation provider.
func NewOpenAICompatProvider() provider.Provider {
	return &openAICompatProvider{
		baseURL: "https://api.openai.com/v1",
		model:   "gpt-4o-mini",
	}
}

func (p *openAICompatProvider) ID() string   { return domain.OriginLLM }
func (p *openAICompatProvider) Name() string { return "OpenAI / 兼容大模型 (OpenAI · Ollama · vLLM)" }
func (p *openAICompatProvider) Description() string {
	return "调用 OpenAI、Ollama、vLLM 等兼容 /chat/completions 接口的大模型进行上下文感知翻译"
}
func (p *openAICompatProvider) Kind() provider.Kind { return provider.KindEngine }

func (p *openAICompatProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:         "base_url",
			Label:       "API Base URL",
			Type:        provider.FieldText,
			Default:     "https://api.openai.com/v1",
			Placeholder: "https://api.openai.com/v1 或 http://127.0.0.1:11434/v1",
		},
		{
			Key:         "model",
			Label:       "模型名称",
			Type:        provider.FieldText,
			Default:     "gpt-4o-mini",
			Placeholder: "gpt-4o-mini / qwen2.5:14b / deepseek-chat",
		},
		{
			Key:         "api_key",
			Label:       "API Key",
			Type:        provider.FieldSecret,
			Placeholder: "sk-...（本地 Ollama 可留空）",
		},
		{
			Key:         "domain_prompt",
			Label:       "领域提示词",
			Type:        provider.FieldText,
			Default:     "",
			Placeholder: "例如：桌面软件界面本地化，译文简洁自然，严格保留 %1 占位符",
		},
		{
			Key:     "use_proxy",
			Label:   "使用代理",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *openAICompatProvider) Configure(values map[string]string, pool *netpool.Pool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool = pool
	if u := strings.TrimSpace(values["base_url"]); u != "" {
		p.baseURL = strings.TrimRight(u, "/")
	} else {
		p.baseURL = "https://api.openai.com/v1"
	}
	if m := strings.TrimSpace(values["model"]); m != "" {
		p.model = m
	} else {
		p.model = "gpt-4o-mini"
	}
	p.apiKey = strings.TrimSpace(values["api_key"])
	p.domainPrompt = strings.TrimSpace(values["domain_prompt"])
	p.useProxy = values["use_proxy"] == "true"
	return nil
}

func (p *openAICompatProvider) getConfig() (baseURL, model, apiKey, domainPrompt string, client *http.Client, err error) {
	p.mu.RLock()
	pool := p.pool
	useProxy := p.useProxy
	baseURL = p.baseURL
	model = p.model
	apiKey = p.apiKey
	domainPrompt = p.domainPrompt
	p.mu.RUnlock()

	if pool == nil {
		pool = netpool.New(netpool.ProxyConfig{})
	}
	client, err = pool.Client(useProxy)
	return
}

func (p *openAICompatProvider) Check(ctx context.Context) error {
	models, err := p.DiscoverModels(ctx)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return fmt.Errorf("未发现可用模型")
	}
	return nil
}

// DiscoverModels queries /models (OpenAI standard) with fallback to /api/tags (Ollama native).
func (p *openAICompatProvider) DiscoverModels(ctx context.Context) ([]string, error) {
	baseURL, _, apiKey, _, client, err := p.getConfig()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if resp.StatusCode == http.StatusOK {
			var parsed struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &parsed) == nil && len(parsed.Data) > 0 {
				models := make([]string, 0, len(parsed.Data))
				for _, m := range parsed.Data {
					if m.ID != "" {
						models = append(models, m.ID)
					}
				}
				return models, nil
			}
		}
	}

	// Fallback: Ollama native /api/tags
	u, parseErr := url.Parse(baseURL)
	if parseErr != nil {
		return nil, fmt.Errorf("查询模型列表失败: %w", parseErr)
	}
	ollamaURL := fmt.Sprintf("%s://%s/api/tags", u.Scheme, u.Host)
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaURL, nil)
	if err != nil {
		return nil, err
	}
	resp2, err := client.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("查询模型列表失败 (%s): %w", baseURL, err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(io.LimitReader(resp2.Body, 2<<20))
	if resp2.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询模型列表返回 HTTP %d: %s", resp2.StatusCode, truncateStr(string(body2), 160))
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body2, &tags); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	models := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		if m.Name != "" {
			models = append(models, m.Name)
		}
	}
	return models, nil
}

func (p *openAICompatProvider) Translate(ctx context.Context, req provider.Request) (map[string]string, error) {
	if len(req.Items) == 0 {
		return map[string]string{}, nil
	}
	baseURL, model, apiKey, domainPrompt, client, err := p.getConfig()
	if err != nil {
		return nil, err
	}

	targetLang := req.TargetLocale.Name
	if targetLang == "" {
		targetLang = req.TargetLocale.ID
	}
	if req.EngineCode != "" {
		targetLang = fmt.Sprintf("%s (%s)", targetLang, req.EngineCode)
	}

	out := make(map[string]string, len(req.Items))
	const batchSize = 20
	for start := 0; start < len(req.Items); start += batchSize {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		end := min(start+batchSize, len(req.Items))
		chunk := req.Items[start:end]

		resMap, err := p.translateChunkWithRetry(ctx, client, baseURL, model, apiKey, domainPrompt, targetLang, chunk)
		if err != nil {
			return out, err
		}
		for k, v := range resMap {
			if v != "" {
				out[k] = v
			}
		}
	}
	return out, nil
}

func (p *openAICompatProvider) translateChunkWithRetry(
	ctx context.Context,
	client *http.Client,
	baseURL, model, apiKey, domainPrompt, targetLang string,
	chunk []provider.SourceItem,
) (map[string]string, error) {
	type promptEntry struct {
		ID      string `json:"id"`
		Context string `json:"context,omitempty"`
		Source  string `json:"source"`
		Ref     string `json:"ref,omitempty"`
	}

	var refLabel string
	for _, it := range chunk {
		if it.RefLocale != "" {
			refLabel = it.RefLocale
			break
		}
	}

	entries := make([]promptEntry, len(chunk))
	for i, it := range chunk {
		src := it.Key.Source
		if src == "" {
			src = it.RefText
		}
		var ref string
		if it.RefText != "" && it.RefText != src {
			ref = it.RefText
		}
		entries[i] = promptEntry{
			ID:      it.ID,
			Context: it.Key.Context,
			Source:  src,
			Ref:     ref,
		}
	}
	entriesJSON, _ := json.Marshal(entries)

	var sysPrompt strings.Builder
	sysPrompt.WriteString("You are a professional software UI localization translator.\n")
	if domainPrompt != "" {
		sysPrompt.WriteString("Domain guidance: ")
		sysPrompt.WriteString(domainPrompt)
		sysPrompt.WriteByte('\n')
	}
	sysPrompt.WriteString(fmt.Sprintf("Translate each `source` string into %s.\n", targetLang))
	if refLabel != "" {
		sysPrompt.WriteString(fmt.Sprintf("Optional `ref` fields provide reference translations in %s for semantic context only.\n", refLabel))
	}
	sysPrompt.WriteString("1. Preserve all placeholder tokens such as [[0]], [[1]], %1, %n, and punctuation exactly.\n")
	sysPrompt.WriteString("2. Return ONLY a valid JSON object mapping each `id` to its translated string.\n")

	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": sysPrompt.String()},
			{"role": "user", "content": string(entriesJSON)},
		},
		"temperature": 0.1,
	}
	reqBody, _ := json.Marshal(payload)

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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
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
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
		}

		var chatResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &chatResp); err != nil {
			return nil, fmt.Errorf("invalid chat response JSON: %w", err)
		}
		if len(chatResp.Choices) == 0 {
			return nil, fmt.Errorf("empty choices in chat response")
		}
		return extractJSONMap(chatResp.Choices[0].Message.Content)
	}

	return nil, lastErr
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

func extractJSONMap(raw string) (map[string]string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = s[idx+1:]
		}
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}

	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in model output")
	}
	s = s[start : end+1]

	var anyMap map[string]any
	if err := json.Unmarshal([]byte(s), &anyMap); err != nil {
		return nil, fmt.Errorf("failed to parse model JSON map: %w", err)
	}
	out := make(map[string]string, len(anyMap))
	for k, v := range anyMap {
		switch tv := v.(type) {
		case string:
			out[k] = tv
		case float64:
			out[k] = strconv.FormatFloat(tv, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(tv)
		}
	}
	return out, nil
}
