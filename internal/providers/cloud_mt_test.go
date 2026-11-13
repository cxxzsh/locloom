package providers

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

func TestDeepLOfficialAndAutoHost(t *testing.T) {
	ep, err := resolveDeepLEndpoint("official", "", "my-key:fx")
	if err != nil || ep != defaultDeepLFreeURL {
		t.Fatalf("expected free endpoint %q, got %q (err=%v)", defaultDeepLFreeURL, ep, err)
	}
	ep, err = resolveDeepLEndpoint("official", defaultDeepLXURL, "my-key:fx")
	if err != nil || ep != defaultDeepLFreeURL {
		t.Fatalf("expected free endpoint for default 1188 url, got %q (err=%v)", ep, err)
	}
	ep, err = resolveDeepLEndpoint("official", defaultDeepLXURL, "paid-key")
	if err != nil || ep != defaultDeepLPaidURL {
		t.Fatalf("expected paid endpoint %q, got %q (err=%v)", defaultDeepLPaidURL, ep, err)
	}

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "DeepL-Auth-Key test-key:fx" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if calls.Add(1) == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		var reqBody struct {
			Text       []string `json:"text"`
			TargetLang string   `json:"target_lang"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if reqBody.TargetLang != "ZH" || len(reqBody.Text) != 2 {
			http.Error(w, "unexpected payload", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"translations":[{"text":"你好"},{"text":"世界"}]}`)
	}))
	defer srv.Close()

	p := NewDeepLProvider()
	if err := p.Configure(map[string]string{
		"mode":       "official",
		"base_url":   srv.URL,
		"api_key":    "test-key:fx",
		"batch_size": "25",
	}, netpool.New(netpool.ProxyConfig{})); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	res, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "zh-CN"},
		EngineCode:   "zh",
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Source: "Hello"}},
			{ID: "2", Key: domain.EntryKey{Source: "World"}},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls (1 retry on 429), got %d", calls.Load())
	}
	if res["1"] != "你好" || res["2"] != "世界" {
		t.Fatalf("unexpected translations: %#v", res)
	}
}

func TestDeepLXModeAndRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer lx-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		n := calls.Add(1)
		if n == 1 {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		var reqBody struct {
			Text       string `json:"text"`
			SourceLang string `json:"source_lang"`
			TargetLang string `json:"target_lang"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if reqBody.SourceLang != "auto" || reqBody.TargetLang != "JA" {
			http.Error(w, "bad lang", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"code":200,"data":"ja:%s"}`, reqBody.Text)
	}))
	defer srv.Close()

	p := NewDeepLProvider()
	if err := p.Configure(map[string]string{
		"mode":     "deeplx",
		"base_url": srv.URL,
		"api_key":  "lx-secret",
	}, netpool.New(netpool.ProxyConfig{})); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	res, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "ja"},
		EngineCode:   "ja",
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Source: "Open"}},
			{ID: "2", Key: domain.EntryKey{Source: "Close"}},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls (1 retry + 2 items), got %d", calls.Load())
	}
	if res["1"] != "ja:Open" || res["2"] != "ja:Close" {
		t.Fatalf("unexpected translations: %#v", res)
	}
}

func TestBaiduTranslateSignBatchRetryAndSplit(t *testing.T) {
	const (
		appID     = "202601010001"
		appSecret = "secret-xyz"
	)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, err := url.ParseQuery(string(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q := vals.Get("q")
		salt := vals.Get("salt")
		gotSign := vals.Get("sign")
		wantSign := fmt.Sprintf("%x", md5.Sum([]byte(appID+q+salt+appSecret)))
		if gotSign != wantSign {
			http.Error(w, fmt.Sprintf("bad sign: got %s want %s", gotSign, wantSign), http.StatusBadRequest)
			return
		}

		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// Simulate QPS limit (54003) on first attempt.
			_, _ = io.WriteString(w, `{"error_code":"54003","error_msg":"Invalid Access Limit"}`)
			return
		}

		lines := strings.Split(q, "\n")
		if len(lines) == 4 {
			// Return mismatched count on 4-item batch to force binary split.
			_, _ = io.WriteString(w, `{"trans_result":[{"src":"all","dst":"only-one"}]}`)
			return
		}

		type trItem struct {
			Src string `json:"src"`
			Dst string `json:"dst"`
		}
		var results []trItem
		for _, line := range lines {
			results = append(results, trItem{Src: line, Dst: "zh:" + line})
		}
		respBytes, _ := json.Marshal(map[string]any{"trans_result": results})
		_, _ = w.Write(respBytes)
	}))
	defer srv.Close()

	p := NewBaiduProvider()
	if err := p.Configure(map[string]string{
		"app_id":       appID,
		"app_secret":   appSecret,
		"base_url":     srv.URL,
		"qps_delay_ms": "0",
	}, netpool.New(netpool.ProxyConfig{})); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	res, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "zh-CN"},
		EngineCode:   "zh",
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Source: "Hello"}},
			{ID: "2", Key: domain.EntryKey{Source: "World"}},
			{ID: "3", Key: domain.EntryKey{Source: "Save"}},
			{ID: "4", Key: domain.EntryKey{Source: "Exit"}},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if res["1"] != "zh:Hello" || res["2"] != "zh:World" || res["3"] != "zh:Save" || res["4"] != "zh:Exit" {
		t.Fatalf("unexpected translations: %#v", res)
	}
	if calls.Load() < 4 {
		t.Fatalf("expected retry + split calls (>=4), got %d", calls.Load())
	}
}

func TestNiutransBatchSplitAndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			From    string `json:"from"`
			To      string `json:"to"`
			APIKey  string `json:"apikey"`
			SrcText string `json:"src_text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if reqBody.APIKey != "niu-key" || reqBody.From != "auto" || reqBody.To != "zh" {
			http.Error(w, "invalid params", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if reqBody.SrcText == "TRIGGER_ERROR" {
			_, _ = io.WriteString(w, `{"error_code":"13001","error_msg":"Invalid apikey"}`)
			return
		}

		lines := strings.Split(reqBody.SrcText, "\n")
		if len(lines) == 4 {
			// Return wrong line count to trigger binary-split fallback.
			_, _ = io.WriteString(w, `{"tgt_text":"only-one-line"}`)
			return
		}
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = "niu:" + l
		}
		respBytes, _ := json.Marshal(map[string]string{"tgt_text": strings.Join(out, "\n")})
		_, _ = w.Write(respBytes)
	}))
	defer srv.Close()

	p := NewNiutransProvider()
	if err := p.Configure(map[string]string{
		"api_key":  "niu-key",
		"base_url": srv.URL,
	}, netpool.New(netpool.ProxyConfig{})); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	res, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "zh-CN"},
		EngineCode:   "zh",
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Source: "File"}},
			{ID: "2", Key: domain.EntryKey{Source: "Edit"}},
			{ID: "3", Key: domain.EntryKey{Source: "View"}},
			{ID: "4", Key: domain.EntryKey{Source: "Help"}},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if res["1"] != "niu:File" || res["2"] != "niu:Edit" || res["3"] != "niu:View" || res["4"] != "niu:Help" {
		t.Fatalf("unexpected translations: %#v", res)
	}

	_, err = p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "zh-CN"},
		EngineCode:   "zh",
		Items: []provider.SourceItem{
			{ID: "err", Key: domain.EntryKey{Source: "TRIGGER_ERROR"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Invalid apikey") {
		t.Fatalf("expected error_msg failure, got %v", err)
	}
}

func TestCloudMTLocalizedProviders(t *testing.T) {
	cases := []struct {
		p      provider.Provider
		wantID string
	}{
		{NewDeepLProvider(), domain.OriginDeepL},
		{NewBaiduProvider(), domain.OriginBaidu},
		{NewNiutransProvider(), domain.OriginNiutrans},
	}

	for _, tc := range cases {
		if tc.p.ID() != tc.wantID {
			t.Fatalf("expected ID %q, got %q", tc.wantID, tc.p.ID())
		}
		if tc.p.Kind() != provider.KindEngine {
			t.Fatalf("expected KindEngine for %s, got %s", tc.wantID, tc.p.Kind())
		}

		lp, ok := tc.p.(provider.LocalizedProvider)
		if !ok {
			t.Fatalf("%s does not implement LocalizedProvider", tc.wantID)
		}

		nameZH := lp.LocalizedName("zh")
		nameEN := lp.LocalizedName("en")
		if nameZH == "" || nameEN == "" || nameZH == nameEN {
			t.Fatalf("%s localized names invalid: zh=%q en=%q", tc.wantID, nameZH, nameEN)
		}

		descZH := lp.LocalizedDescription("zh")
		descEN := lp.LocalizedDescription("en")
		if descZH == "" || descEN == "" || descZH == descEN {
			t.Fatalf("%s localized descriptions invalid: zh=%q en=%q", tc.wantID, descZH, descEN)
		}

		schemaZH := lp.LocalizedConfigSchema("zh")
		schemaEN := lp.LocalizedConfigSchema("en")
		if len(schemaZH) == 0 || len(schemaZH) != len(schemaEN) {
			t.Fatalf("%s schema length mismatch: zh=%d en=%d", tc.wantID, len(schemaZH), len(schemaEN))
		}
		for i := range schemaZH {
			if schemaZH[i].Key != schemaEN[i].Key {
				t.Fatalf("%s schema key mismatch at %d: %q vs %q", tc.wantID, i, schemaZH[i].Key, schemaEN[i].Key)
			}
			if schemaZH[i].Label == "" || schemaEN[i].Label == "" {
				t.Fatalf("%s empty label at %d", tc.wantID, i)
			}
		}
	}
}
