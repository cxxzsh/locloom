package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/xuri/excelize/v2"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

func TestRegisterBuiltins(t *testing.T) {
	reg := provider.NewRegistry()
	RegisterBuiltins(reg)

	for _, id := range []string{"xlsx", "ts_tm", "google_web", "bing_web", "deepl", "mymemory", "libretranslate", "baidu_api", "niutrans", "openai_compat"} {
		if _, ok := reg.Get(id); !ok {
			t.Fatalf("expected built-in provider %q to be registered", id)
		}
	}
}

func TestXLSXProviderMatchingAndConcurrentCache(t *testing.T) {
	dir := t.TempDir()
	xlsxPath := filepath.Join(dir, "glossary.xlsx")

	f := excelize.NewFile()
	sheetName := f.GetSheetName(0)
	_ = f.SetSheetRow(sheetName, "A1", &[]any{"Context", "Source", "Comment", "ja", "fr"})
	_ = f.SetSheetRow(sheetName, "A2", &[]any{"MainWindow", "Save", "menu", "保存(menu)", "Enregistrer(menu)"})
	_ = f.SetSheetRow(sheetName, "A3", &[]any{"MainWindow", "Save", "", "保存", "Enregistrer"})
	_ = f.SetSheetRow(sheetName, "A4", &[]any{"SettingsDialog", "Open", "", "開く", "Ouvrir"})
	if err := f.SaveAs(xlsxPath); err != nil {
		t.Fatalf("SaveAs failed: %v", err)
	}
	_ = f.Close()

	p := NewXLSXProvider()
	if err := p.Configure(map[string]string{
		"file_path": xlsxPath,
		"skip_ref":  "true",
	}, nil); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if err := p.Check(context.Background()); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	var wg sync.WaitGroup
	for _, locID := range []string{"ja", "fr"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			out, err := p.Translate(context.Background(), provider.Request{
				TargetLocale: locale.LocaleSpec{ID: id},
				Items: []provider.SourceItem{
					{ID: "1", Key: domain.EntryKey{Context: "MainWindow", Source: "Save", Comment: "menu"}},
					{ID: "2", Key: domain.EntryKey{Context: "MainWindow", Source: "Save", Comment: "toolbar"}},
					{ID: "3", Key: domain.EntryKey{Context: "SettingsDialog", Source: "Open"}},
				},
			})
			if err != nil {
				t.Errorf("Translate(%s) failed: %v", id, err)
				return
			}
			if id == "ja" {
				if out["1"] != "保存(menu)" || out["2"] != "保存" || out["3"] != "開く" {
					t.Errorf("unexpected ja translations: %+v", out)
				}
			} else {
				if out["1"] != "Enregistrer(menu)" || out["2"] != "Enregistrer" || out["3"] != "Ouvrir" {
					t.Errorf("unexpected fr translations: %+v", out)
				}
			}
		}(locID)
	}
	wg.Wait()

	// Workbook without Context column falls back to ("", Source)
	noCtxPath := filepath.Join(dir, "no_ctx.xlsx")
	f2 := excelize.NewFile()
	s2 := f2.GetSheetName(0)
	_ = f2.SetSheetRow(s2, "A1", &[]any{"Source", "de"})
	_ = f2.SetSheetRow(s2, "A2", &[]any{"Cancel", "Abbrechen"})
	if err := f2.SaveAs(noCtxPath); err != nil {
		t.Fatal(err)
	}
	_ = f2.Close()

	_ = p.Configure(map[string]string{"file_path": noCtxPath}, nil)
	out, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "de"},
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Context: "MainWindow", Source: "Cancel"}},
		},
	})
	if err != nil {
		t.Fatalf("Translate no-ctx failed: %v", err)
	}
	if out["1"] != "Abbrechen" {
		t.Fatalf("expected Abbrechen, got %q", out["1"])
	}
}

func TestTSTMProviderMatchingAndFinishedOnly(t *testing.T) {
	dir := t.TempDir()
	tsContent := `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP">
<context>
    <name>MainWindow</name>
    <message>
        <source>Save</source>
        <translation>保存</translation>
    </message>
    <message>
        <source>Draft</source>
        <translation type="unfinished">下書き</translation>
    </message>
</context>
<context>
    <name>FileManager</name>
    <message>
        <source>Delete</source>
        <translation>削除</translation>
    </message>
</context>
</TS>`
	if err := os.WriteFile(filepath.Join(dir, "ja.ts"), []byte(tsContent), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewTSTMProvider()
	if err := p.Configure(map[string]string{"tm_path": dir}, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(context.Background()); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	out, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "ja"},
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Context: "MainWindow", Source: "Save"}},
			{ID: "2", Key: domain.EntryKey{Context: "MainWindow", Source: "Draft"}},
			{ID: "3", Key: domain.EntryKey{Context: "OtherCtx", Source: "Delete"}},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if out["1"] != "保存" {
		t.Fatalf("expected 保存, got %q", out["1"])
	}
	if _, exists := out["2"]; exists {
		t.Fatal("unfinished entry should not be returned by ts_tm")
	}
	if out["3"] != "削除" {
		t.Fatalf("expected Source fallback 削除, got %q", out["3"])
	}
}

func TestGoogleWebProviderBatchSplitAndSpacePreservation(t *testing.T) {
	var singleCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if r.URL.Path == "/translate_a/single" {
			singleCalls.Add(1)
			// Simulate line-count drift when batch contains multiple lines ("A\nB")
			if strings.Contains(q, "\n") {
				_ = json.NewEncoder(w).Encode([]any{
					[]any{[]any{"ONLY_ONE_LINE", q}},
				})
				return
			}
			var translated string
			switch q {
			case "Hello, [[0]]!":
				translated = "你好，[[0]]！"
			case "Name: ":
				translated = "名称： "
			case "Save":
				translated = "保存"
			default:
				translated = "T:" + q
			}
			_ = json.NewEncoder(w).Encode([]any{
				[]any{[]any{translated, q}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := NewGoogleWebProvider()
	if err := p.Configure(map[string]string{
		"host":       srv.URL,
		"endpoint":   "gtx",
		"batch_size": "10",
		"use_proxy":  "false",
	}, netpool.New(netpool.ProxyConfig{})); err != nil {
		t.Fatal(err)
	}

	if err := p.Check(context.Background()); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	out, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "zh_CN"},
		EngineCode:   "zh-CN",
		Items: []provider.SourceItem{
			{ID: "1", RefLocale: "en", RefText: "Name: "},
			{ID: "2", RefLocale: "en", RefText: "Save"},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if out["1"] != "名称： " {
		t.Fatalf("expected trailing space preserved %q, got %q", "名称： ", out["1"])
	}
	if out["2"] != "保存" {
		t.Fatalf("expected 保存, got %q", out["2"])
	}
	if singleCalls.Load() < 3 {
		t.Fatalf("expected binary-split fallback to make >=3 calls, got %d", singleCalls.Load())
	}
}

func TestOpenAICompatProviderPromptRetryAndDiscovery(t *testing.T) {
	var chatAttempts atomic.Int32
	var capturedSystemPrompt string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "gpt-4o-mini"}, {"id": "qwen2.5:14b"}},
			})
		case "/v1/chat/completions":
			attempt := chatAttempts.Add(1)
			if attempt == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = fmt.Fprint(w, "rate limited")
				return
			}
			var body struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Messages) > 0 {
				capturedSystemPrompt = body.Messages[0].Content
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]string{
							"content": "```json\n{\"1\": \"Enregistrer [[0]]\"}\n```",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider()
	if err := p.Configure(map[string]string{
		"base_url":      srv.URL + "/v1",
		"model":         "gpt-4o-mini",
		"api_key":       "test-key",
		"domain_prompt": "Desktop software UI",
		"use_proxy":     "false",
	}, netpool.New(netpool.ProxyConfig{})); err != nil {
		t.Fatal(err)
	}

	disc, ok := p.(provider.ModelDiscoverer)
	if !ok {
		t.Fatal("expected OpenAICompatProvider to implement ModelDiscoverer")
	}
	models, err := disc.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels failed: %v", err)
	}
	if len(models) != 2 || models[0] != "gpt-4o-mini" {
		t.Fatalf("unexpected models: %v", models)
	}

	out, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "fr", Name: "French"},
		EngineCode:   "fr",
		Items: []provider.SourceItem{
			{ID: "1", Key: domain.EntryKey{Context: "MainWindow", Source: "Save %1"}, RefLocale: "en_US", RefText: "Save [[0]]"},
		},
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if chatAttempts.Load() != 2 {
		t.Fatalf("expected retry on 429 (2 attempts), got %d", chatAttempts.Load())
	}
	if out["1"] != "Enregistrer [[0]]" {
		t.Fatalf("unexpected translation: %+v", out)
	}
	if !strings.Contains(capturedSystemPrompt, "en_US") {
		t.Fatalf("expected dynamic RefLocale en_US in prompt, got: %s", capturedSystemPrompt)
	}
	if strings.Contains(capturedSystemPrompt, "zh_CN") {
		t.Fatalf("prompt should not hardcode zh_CN: %s", capturedSystemPrompt)
	}

	// Verify Ollama /api/tags fallback when /models returns 404
	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "llama3.2:latest"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ollamaSrv.Close()

	_ = p.Configure(map[string]string{"base_url": ollamaSrv.URL + "/v1"}, netpool.New(netpool.ProxyConfig{}))
	ollamaModels, err := disc.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("Ollama DiscoverModels fallback failed: %v", err)
	}
	if len(ollamaModels) != 1 || ollamaModels[0] != "llama3.2:latest" {
		t.Fatalf("unexpected Ollama models: %v", ollamaModels)
	}
}

func TestBuiltinsLocalizedProvider(t *testing.T) {
	reg := provider.NewRegistry()
	RegisterBuiltins(reg)

	for _, p := range reg.All() {
		nameEn := provider.ProviderName(p, "en")
		nameZh := provider.ProviderName(p, "zh")
		if nameEn == "" || nameZh == "" {
			t.Fatalf("provider %s returned empty localized name: en=%q zh=%q", p.ID(), nameEn, nameZh)
		}
		if nameEn == nameZh {
			t.Fatalf("provider %s en and zh names are identical: %q", p.ID(), nameEn)
		}

		descEn := provider.ProviderDescription(p, "en")
		descZh := provider.ProviderDescription(p, "zh")
		if descEn == "" || descZh == "" || descEn == descZh {
			t.Fatalf("provider %s localized descriptions invalid: en=%q zh=%q", p.ID(), descEn, descZh)
		}

		schemaEn := provider.ProviderSchema(p, "en")
		schemaZh := provider.ProviderSchema(p, "zh")
		if len(schemaEn) == 0 || len(schemaEn) != len(schemaZh) {
			t.Fatalf("provider %s schema length mismatch: en=%d zh=%d", p.ID(), len(schemaEn), len(schemaZh))
		}
		for i := range schemaEn {
			if schemaEn[i].Key != schemaZh[i].Key {
				t.Fatalf("provider %s field %d key mismatch: %q vs %q", p.ID(), i, schemaEn[i].Key, schemaZh[i].Key)
			}
			if schemaEn[i].Label == "" || schemaZh[i].Label == "" {
				t.Fatalf("provider %s field %s has empty label", p.ID(), schemaEn[i].Key)
			}
		}
	}
}
