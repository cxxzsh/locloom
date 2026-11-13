package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/provider"
)

func TestBingWebEdgeFreeAndRetry429(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.URL.Query().Get("to"); got != "zh-Hans" {
			t.Fatalf("expected to=zh-Hans, got %q", got)
		}
		if got := r.URL.Query().Get("isEnterpriseClient"); got != "false" {
			t.Fatalf("expected isEnterpriseClient=false, got %q", got)
		}
		if ua := r.Header.Get("User-Agent"); !strings.Contains(ua, "Edg/") {
			t.Fatalf("expected Edge User-Agent, got %q", ua)
		}

		// Trigger 429 on the first Translate call (attempt 1) to verify retry with backoff.
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var texts []string
		if err := json.Unmarshal(raw, &texts); err != nil {
			t.Fatalf("expected []string body, got %s (%v)", string(raw), err)
		}

		type trItem struct {
			Translations []struct {
				Text string `json:"text"`
			} `json:"translations"`
		}
		resp := make([]trItem, len(texts))
		for i, txt := range texts {
			resp[i].Translations = []struct {
				Text string `json:"text"`
			}{{Text: "ZH:" + txt}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewBingWebProvider()
	if p.ID() != domain.OriginBing || p.Kind() != provider.KindEngine {
		t.Fatalf("unexpected provider identity: id=%s kind=%s", p.ID(), p.Kind())
	}
	if err := p.Configure(map[string]string{
		"mode":         "edge_free",
		"endpoint_url": srv.URL,
		"batch_size":   "10",
	}, nil); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	got, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "zh_CN"},
		EngineCode:   "zh-Hans",
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Source: "Hello, [[0]]!"}},
			{ID: "2", RefText: "  Save  "},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 HTTP attempts (1 retry after 429), got %d", attempts.Load())
	}
	if got["1"] != "ZH:Hello, [[0]]!" {
		t.Fatalf("unexpected item 1 translation: %q", got["1"])
	}
	if got["2"] != "ZH:  Save  " {
		t.Fatalf("unexpected item 2 translation (spaces must be preserved): %q", got["2"])
	}

	if err := p.Check(context.Background()); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
}

func TestBingWebAzureKeyMode(t *testing.T) {
	p := NewBingWebProvider()
	if err := p.Configure(map[string]string{
		"mode": "azure_key",
	}, nil); err == nil {
		t.Fatal("expected error when azure_key mode has empty api_key")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api-version"); got != "3.0" {
			t.Fatalf("expected api-version=3.0, got %q", got)
		}
		if got := r.URL.Query().Get("to"); got != "ja" && got != "zh-Hans" {
			t.Fatalf("unexpected to param: %q", got)
		}
		if got := r.Header.Get("Ocp-Apim-Subscription-Key"); got != "secret-key" {
			t.Fatalf("expected subscription key header, got %q", got)
		}
		if got := r.Header.Get("Ocp-Apim-Subscription-Region"); got != "eastasia" {
			t.Fatalf("expected region header, got %q", got)
		}

		raw, _ := io.ReadAll(r.Body)
		var items []struct {
			Text string `json:"Text"`
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			t.Fatalf("expected azure body objects, got %s (%v)", string(raw), err)
		}

		type trItem struct {
			Translations []struct {
				Text string `json:"text"`
			} `json:"translations"`
		}
		resp := make([]trItem, len(items))
		for i, it := range items {
			resp[i].Translations = []struct {
				Text string `json:"text"`
			}{{Text: "AZ:" + it.Text}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	if err := p.Configure(map[string]string{
		"mode":         "azure_key",
		"endpoint_url": srv.URL,
		"api_key":      "secret-key",
		"region":       "eastasia",
	}, nil); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	got, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "ja"},
		EngineCode:   "ja",
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Source: "Open [[0]]"}},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if got["1"] != "AZ:Open [[0]]" {
		t.Fatalf("unexpected azure translation: %q", got["1"])
	}
	if err := p.Check(context.Background()); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
}

func TestMyMemoryProviderAndWarningRejection(t *testing.T) {
	var returnWarning atomic.Bool
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		q := r.URL.Query()
		if got := q.Get("de"); got != "dev@example.com" {
			t.Fatalf("expected de=dev@example.com, got %q", got)
		}
		if got := q.Get("key"); got != "mm-key" {
			t.Fatalf("expected key=mm-key, got %q", got)
		}
		if lp := q.Get("langpair"); lp != "en|zh-CN" {
			t.Fatalf("expected langpair=en|zh-CN, got %q", lp)
		}

		txt := "你好 " + q.Get("q")
		if returnWarning.Load() {
			txt = "MYMEMORY WARNING: YOU USED ALL AVAILABLE FREE TRANSLATIONS FOR TODAY."
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"responseStatus": 200,
			"responseData": map[string]string{
				"translatedText": txt,
			},
		})
	}))
	defer srv.Close()

	p := NewMyMemoryProvider()
	if p.ID() != domain.OriginMyMemory || p.Kind() != provider.KindEngine {
		t.Fatalf("unexpected provider identity: id=%s kind=%s", p.ID(), p.Kind())
	}
	if err := p.Configure(map[string]string{
		"base_url":    srv.URL,
		"source_lang": "en",
		"email":       "dev@example.com",
		"api_key":     "mm-key",
	}, nil); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	got, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "zh_CN"},
		EngineCode:   "zh-CN",
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Source: "Save [[0]]"}},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if got["1"] != "你好 Save [[0]]" {
		t.Fatalf("unexpected translation: %q", got["1"])
	}

	// Same source and target language should skip HTTP call gracefully.
	prevCalls := callCount.Load()
	sameGot, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "en"},
		EngineCode:   "EN",
		Items:        []provider.SourceItem{{ID: "1", Key: domain.EntryKey{Source: "Save"}}},
	})
	if err != nil {
		t.Fatalf("same-language Translate failed: %v", err)
	}
	if sameGot["1"] != "Save" || callCount.Load() != prevCalls {
		t.Fatalf("expected same-language skip without HTTP call, got %v (calls %d->%d)", sameGot, prevCalls, callCount.Load())
	}

	if err := p.Check(context.Background()); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Quota warning must be rejected as an error.
	returnWarning.Store(true)
	_, err = p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "zh_CN"},
		EngineCode:   "zh-CN",
		Items:        []provider.SourceItem{{ID: "1", Key: domain.EntryKey{Source: "Hello"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "MYMEMORY WARNING") {
		t.Fatalf("expected MYMEMORY WARNING error, got %v", err)
	}
}

func TestLibreTranslateModesAndFallback(t *testing.T) {
	t.Run("lingva_mode", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/v1/auto/") {
				t.Fatalf("unexpected lingva path: %s", r.URL.Path)
			}
			parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/v1/auto/"), "/", 2)
			if len(parts) != 2 {
				t.Fatalf("unexpected lingva path segments: %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"translation": parts[0] + ":" + parts[1],
			})
		}))
		defer srv.Close()

		p := NewLibreTranslateProvider()
		if p.ID() != domain.OriginLibre || p.Kind() != provider.KindEngine {
			t.Fatalf("unexpected provider identity: id=%s kind=%s", p.ID(), p.Kind())
		}
		if err := p.Configure(map[string]string{
			"mode":     "lingva",
			"base_url": srv.URL,
		}, nil); err != nil {
			t.Fatalf("Configure failed: %v", err)
		}

		got, err := p.Translate(context.Background(), provider.Request{
			TargetLocale: locale.LocaleSpec{ID: "zh_TW"},
			EngineCode:   "zt",
			Items:        []provider.SourceItem{{ID: "1", Key: domain.EntryKey{Source: "Settings"}}},
		})
		if err != nil {
			t.Fatalf("Translate failed: %v", err)
		}
		if got["1"] != "zh_HANT:Settings" {
			t.Fatalf("expected zt->zh_HANT mapping, got %q", got["1"])
		}
		if err := p.Check(context.Background()); err != nil {
			t.Fatalf("Check failed: %v", err)
		}
	})

	t.Run("libretranslate_batch_array", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/translate" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			var payload struct {
				Q      []string `json:"q"`
				Target string   `json:"target"`
				APIKey string   `json:"api_key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch body: %v", err)
			}
			if payload.APIKey != "lt-secret" {
				t.Fatalf("expected api_key=lt-secret, got %q", payload.APIKey)
			}
			out := make([]string, len(payload.Q))
			for i, s := range payload.Q {
				out[i] = payload.Target + ":" + s
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"translatedText": out,
			})
		}))
		defer srv.Close()

		p := NewLibreTranslateProvider()
		if err := p.Configure(map[string]string{
			"mode":     "libretranslate",
			"base_url": srv.URL,
			"api_key":  "lt-secret",
		}, nil); err != nil {
			t.Fatalf("Configure failed: %v", err)
		}

		got, err := p.Translate(context.Background(), provider.Request{
			TargetLocale: locale.LocaleSpec{ID: "fr"},
			EngineCode:   "fr",
			Items: []provider.SourceItem{
				{ID: "1", Key: domain.EntryKey{Source: "Open"}},
				{ID: "2", Key: domain.EntryKey{Source: "Close"}},
			},
		})
		if err != nil {
			t.Fatalf("Translate failed: %v", err)
		}
		if got["1"] != "fr:Open" || got["2"] != "fr:Close" {
			t.Fatalf("unexpected batch output: %v", got)
		}
		if err := p.Check(context.Background()); err != nil {
			t.Fatalf("Check failed: %v", err)
		}
	})

	t.Run("libretranslate_single_item_fallback", func(t *testing.T) {
		var singleCalls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var probe struct {
				Q      json.RawMessage `json:"q"`
				Target string          `json:"target"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// Reject array "q" to force single-item fallback.
			var single string
			if err := json.Unmarshal(probe.Q, &single); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "q must be a string"})
				return
			}
			singleCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"translatedText": "SINGLE:" + single,
			})
		}))
		defer srv.Close()

		p := NewLibreTranslateProvider()
		if err := p.Configure(map[string]string{
			"mode":     "libretranslate",
			"base_url": srv.URL,
		}, nil); err != nil {
			t.Fatalf("Configure failed: %v", err)
		}

		got, err := p.Translate(context.Background(), provider.Request{
			TargetLocale: locale.LocaleSpec{ID: "de"},
			EngineCode:   "de",
			Items: []provider.SourceItem{
				{ID: "1", Key: domain.EntryKey{Source: "File"}},
				{ID: "2", Key: domain.EntryKey{Source: "Edit"}},
			},
		})
		if err != nil {
			t.Fatalf("Translate fallback failed: %v", err)
		}
		if got["1"] != "SINGLE:File" || got["2"] != "SINGLE:Edit" {
			t.Fatalf("unexpected fallback output: %v", got)
		}
		if singleCalls.Load() != 2 {
			t.Fatalf("expected 2 single-item fallback calls, got %d", singleCalls.Load())
		}
	})
}

func TestWebMTProvidersLocalization(t *testing.T) {
	all := []provider.Provider{
		NewBingWebProvider(),
		NewMyMemoryProvider(),
		NewLibreTranslateProvider(),
	}

	for _, p := range all {
		lp, ok := p.(provider.LocalizedProvider)
		if !ok {
			t.Fatalf("provider %s does not implement LocalizedProvider", p.ID())
		}

		for _, lang := range []string{"zh", "en"} {
			if name := lp.LocalizedName(lang); name == "" {
				t.Fatalf("provider %s LocalizedName(%q) is empty", p.ID(), lang)
			}
			if desc := lp.LocalizedDescription(lang); desc == "" {
				t.Fatalf("provider %s LocalizedDescription(%q) is empty", p.ID(), lang)
			}
			schema := lp.LocalizedConfigSchema(lang)
			if len(schema) == 0 {
				t.Fatalf("provider %s LocalizedConfigSchema(%q) is empty", p.ID(), lang)
			}
			for _, f := range schema {
				if f.Key == "" || f.Label == "" {
					t.Fatalf("provider %s field %+v missing key/label in lang %q", p.ID(), f, lang)
				}
			}
		}

		zhSchema := lp.LocalizedConfigSchema("zh")
		enSchema := lp.LocalizedConfigSchema("en")
		if len(zhSchema) != len(enSchema) {
			t.Fatalf("provider %s schema length mismatch zh=%d en=%d", p.ID(), len(zhSchema), len(enSchema))
		}
		for i := range zhSchema {
			if zhSchema[i].Key != enSchema[i].Key {
				t.Fatalf("provider %s schema key mismatch at %d: %s vs %s", p.ID(), i, zhSchema[i].Key, enSchema[i].Key)
			}
		}
	}
}
