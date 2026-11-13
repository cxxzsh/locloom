package providers

import (
	"context"
	"crypto/md5"
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
	defaultBaiduURL        = "https://fanyi-api.baidu.com/api/trans/vip/translate"
	defaultBaiduQPSDelayMs = 200
	baiduMaxBatchItems     = 20
	baiduMaxBatchBytes     = 4500
)

type baiduProvider struct {
	mu         sync.RWMutex
	appID      string
	appSecret  string
	baseURL    string
	qpsDelayMs int
	useProxy   bool
	pool       *netpool.Pool
}

// NewBaiduProvider creates the built-in Baidu Fanyi Open Platform translation provider.
func NewBaiduProvider() provider.Provider {
	return &baiduProvider{
		baseURL:    defaultBaiduURL,
		qpsDelayMs: defaultBaiduQPSDelayMs,
	}
}

func (p *baiduProvider) ID() string   { return domain.OriginBaidu }
func (p *baiduProvider) Name() string { return "百度翻译开放平台" }
func (p *baiduProvider) Description() string {
	return "调用百度翻译开放平台通用翻译 API（支持批量合并与 QPS 限流控制）"
}

func (p *baiduProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "Baidu Translate API"
	}
	return p.Name()
}

func (p *baiduProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "Translate via Baidu Fanyi Open Platform API with batching and QPS throttling"
	}
	return p.Description()
}

func (p *baiduProvider) Kind() provider.Kind { return provider.KindEngine }

func (p *baiduProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:         "app_id",
			Label:       "APP ID",
			Type:        provider.FieldText,
			Placeholder: "百度翻译开放平台 APP ID",
		},
		{
			Key:         "app_secret",
			Label:       "密钥 (Secret Key)",
			Type:        provider.FieldSecret,
			Placeholder: "百度翻译开放平台密钥",
		},
		{
			Key:     "base_url",
			Label:   "接口地址",
			Type:    provider.FieldText,
			Default: defaultBaiduURL,
		},
		{
			Key:     "qps_delay_ms",
			Label:   "请求间隔 (ms)",
			Type:    provider.FieldText,
			Default: "200",
			Help:    "标准版/免费版 QPS 限流间隔毫秒数（0-2000）",
		},
		{
			Key:     "use_proxy",
			Label:   "使用代理",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *baiduProvider) LocalizedConfigSchema(lang string) []provider.ConfigField {
	if lang != "en" {
		return p.ConfigSchema()
	}
	return []provider.ConfigField{
		{
			Key:         "app_id",
			Label:       "APP ID",
			Type:        provider.FieldText,
			Placeholder: "Baidu Fanyi Open Platform APP ID",
		},
		{
			Key:         "app_secret",
			Label:       "Secret Key",
			Type:        provider.FieldSecret,
			Placeholder: "Baidu Fanyi Open Platform Secret Key",
		},
		{
			Key:     "base_url",
			Label:   "Base URL",
			Type:    provider.FieldText,
			Default: defaultBaiduURL,
		},
		{
			Key:     "qps_delay_ms",
			Label:   "QPS Delay (ms)",
			Type:    provider.FieldText,
			Default: "200",
			Help:    "Optional delay between batch requests for free/standard QPS tiers (0-2000)",
		},
		{
			Key:     "use_proxy",
			Label:   "Use Proxy",
			Type:    provider.FieldBool,
			Default: "false",
		},
	}
}

func (p *baiduProvider) Configure(values map[string]string, pool *netpool.Pool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool = pool

	p.appID = strings.TrimSpace(values["app_id"])
	p.appSecret = strings.TrimSpace(values["app_secret"])
	if base := strings.TrimSpace(values["base_url"]); base != "" {
		p.baseURL = strings.TrimRight(base, "/")
	} else {
		p.baseURL = defaultBaiduURL
	}
	p.useProxy = values["use_proxy"] == "true"

	if d, err := strconv.Atoi(strings.TrimSpace(values["qps_delay_ms"])); err == nil {
		if d < 0 {
			d = 0
		} else if d > 2000 {
			d = 2000
		}
		p.qpsDelayMs = d
	} else {
		p.qpsDelayMs = defaultBaiduQPSDelayMs
	}
	return nil
}

func (p *baiduProvider) getConfig() (appID, appSecret, baseURL string, qpsDelayMs int, client *http.Client, err error) {
	p.mu.RLock()
	appID = p.appID
	appSecret = p.appSecret
	baseURL = p.baseURL
	qpsDelayMs = p.qpsDelayMs
	useProxy := p.useProxy
	pool := p.pool
	p.mu.RUnlock()

	if appID == "" || appSecret == "" {
		return "", "", "", 0, nil, fmt.Errorf("baidu_api requires both app_id and app_secret")
	}
	if baseURL == "" {
		baseURL = defaultBaiduURL
	}
	if pool == nil {
		pool = netpool.New(netpool.ProxyConfig{})
	}
	client, err = pool.Client(useProxy)
	if err != nil {
		return "", "", "", 0, nil, err
	}
	return appID, appSecret, baseURL, qpsDelayMs, client, nil
}

func (p *baiduProvider) Check(ctx context.Context) error {
	appID, appSecret, baseURL, _, client, err := p.getConfig()
	if err != nil {
		return err
	}
	res, err := callBaiduBatchWithRetry(ctx, client, baseURL, appID, appSecret, "auto", "zh", "Hello")
	if err != nil {
		return err
	}
	if len(res) == 0 || res[0] == "" {
		return fmt.Errorf("baidu_api returned empty smoke check result")
	}
	return nil
}

func (p *baiduProvider) Translate(ctx context.Context, req provider.Request) (map[string]string, error) {
	if len(req.Items) == 0 {
		return map[string]string{}, nil
	}
	appID, appSecret, baseURL, qpsDelayMs, client, err := p.getConfig()
	if err != nil {
		return nil, err
	}

	tl := req.EngineCode
	if tl == "" {
		tl = req.TargetLocale.ID
	}

	out := make(map[string]string, len(req.Items))
	batchIdx := 0
	for start := 0; start < len(req.Items); {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if batchIdx > 0 && qpsDelayMs > 0 {
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(time.Duration(qpsDelayMs) * time.Millisecond):
			}
		}

		// Pack up to baiduMaxBatchItems while keeping joined payload <= baiduMaxBatchBytes.
		end := start
		byteCount := 0
		for end < len(req.Items) && (end-start) < baiduMaxBatchItems {
			txt := itemSourceText(req.Items[end])
			added := len(txt)
			if end > start {
				added++ // "\n" separator
			}
			if end > start && byteCount+added > baiduMaxBatchBytes {
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

		translated, err := translateBaiduWithFallback(ctx, client, baseURL, appID, appSecret, "auto", tl, texts)
		if err != nil {
			return out, err
		}
		for i, it := range chunk {
			if i < len(translated) && translated[i] != "" {
				out[it.ID] = strings.TrimSuffix(translated[i], "\r")
			}
		}
		start = end
		batchIdx++
	}
	return out, nil
}

func translateBaiduWithFallback(
	ctx context.Context,
	client *http.Client,
	baseURL, appID, appSecret, from, to string,
	items []string,
) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) == 1 {
		res, err := callBaiduBatchWithRetry(ctx, client, baseURL, appID, appSecret, from, to, items[0])
		if err != nil {
			return nil, err
		}
		if len(res) == 0 {
			return []string{""}, nil
		}
		return []string{strings.Join(res, "\n")}, nil
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
		if len(joined) <= baiduMaxBatchBytes {
			res, err := callBaiduBatchWithRetry(ctx, client, baseURL, appID, appSecret, from, to, joined)
			if err != nil {
				return nil, err
			}
			if len(res) == len(items) {
				return res, nil
			}
			// Paragraph count mismatch: fall through to binary split.
		}
	}

	mid := len(items) / 2
	left, err := translateBaiduWithFallback(ctx, client, baseURL, appID, appSecret, from, to, items[:mid])
	if err != nil {
		return nil, err
	}
	right, err := translateBaiduWithFallback(ctx, client, baseURL, appID, appSecret, from, to, items[mid:])
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func baiduSign(appID, query, salt, appSecret string) string {
	sum := md5.Sum([]byte(appID + query + salt + appSecret))
	return fmt.Sprintf("%x", sum)
}

func callBaiduBatchWithRetry(
	ctx context.Context,
	client *http.Client,
	baseURL, appID, appSecret, from, to, query string,
) ([]string, error) {
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

		salt := strconv.FormatInt(time.Now().UnixNano(), 10)
		sign := baiduSign(appID, query, salt, appSecret)

		form := url.Values{}
		form.Set("q", query)
		form.Set("from", from)
		form.Set("to", to)
		form.Set("appid", appID)
		form.Set("salt", salt)
		form.Set("sign", sign)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
			ErrorCode   string `json:"error_code"`
			ErrorMsg    string `json:"error_msg"`
			TransResult []struct {
				Src string `json:"src"`
				Dst string `json:"dst"`
			} `json:"trans_result"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("invalid baidu response JSON: %w", err)
		}
		if parsed.ErrorCode == "54003" {
			lastErr = fmt.Errorf("baidu QPS limit (54003): %s", parsed.ErrorMsg)
			continue
		}
		if parsed.ErrorCode != "" && parsed.ErrorCode != "52000" {
			return nil, fmt.Errorf("baidu translate error %s: %s", parsed.ErrorCode, parsed.ErrorMsg)
		}

		out := make([]string, len(parsed.TransResult))
		for i, tr := range parsed.TransResult {
			out[i] = tr.Dst
		}
		return out, nil
	}

	return nil, lastErr
}
