package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"locloom/internal/domain"
)

func TestDefaultValues(t *testing.T) {
	cfg := Default()
	if cfg.LeftPaneWidth != 340 {
		t.Fatalf("expected LeftPaneWidth 340, got %v", cfg.LeftPaneWidth)
	}
	if cfg.BottomPaneHeight != 230 {
		t.Fatalf("expected BottomPaneHeight 230, got %v", cfg.BottomPaneHeight)
	}
	if cfg.MatrixViewMode != 0 {
		t.Fatalf("expected MatrixViewMode 0, got %d", cfg.MatrixViewMode)
	}
	if cfg.FilePattern != "{id}.ts" {
		t.Fatalf("expected FilePattern {id}.ts, got %q", cfg.FilePattern)
	}
	if cfg.WriteMode != domain.ModeFillUnfinished {
		t.Fatalf("expected ModeFillUnfinished, got %v", cfg.WriteMode)
	}
	if cfg.AppendContext != "MainWindow" {
		t.Fatalf("expected AppendContext MainWindow, got %q", cfg.AppendContext)
	}
	if !cfg.CreateBackup {
		t.Fatal("expected CreateBackup true")
	}
	if cfg.Concurrency != 4 {
		t.Fatalf("expected Concurrency 4, got %d", cfg.Concurrency)
	}
	if cfg.Proxy.ProxyURL != "" {
		t.Fatalf("expected empty ProxyURL, got %q", cfg.Proxy.ProxyURL)
	}
	wantOrder := []string{"xlsx", "ts_tm", "google_web", "openai_compat"}
	if !slices.Equal(cfg.ProviderOrder, wantOrder) {
		t.Fatalf("unexpected ProviderOrder: %v", cfg.ProviderOrder)
	}
	wantActive := []string{"xlsx", "ts_tm", "google_web"}
	if !slices.Equal(cfg.ActiveProviders, wantActive) {
		t.Fatalf("unexpected ActiveProviders: %v", cfg.ActiveProviders)
	}
	if cfg.ProviderConfigs["openai_compat"]["domain_prompt"] != "" {
		t.Fatalf("expected empty domain_prompt, got %q", cfg.ProviderConfigs["openai_compat"]["domain_prompt"])
	}
}

func TestCloneDeepCopy(t *testing.T) {
	orig := Default()
	orig.Locales[0].EngineCodes = map[string]string{"google_web": "zh-CN"}

	cloned := orig.Clone()
	cloned.ActiveProviders[0] = "mutated"
	cloned.ProviderOrder[0] = "mutated"
	cloned.ProviderConfigs["openai_compat"]["model"] = "other-model"
	cloned.Locales[0].EngineCodes["google_web"] = "ja"
	cloned.Profiles[0].LocaleIDs[0] = "xx"

	if orig.ActiveProviders[0] == "mutated" {
		t.Fatal("ActiveProviders was not deep-copied")
	}
	if orig.ProviderOrder[0] == "mutated" {
		t.Fatal("ProviderOrder was not deep-copied")
	}
	if orig.ProviderConfigs["openai_compat"]["model"] == "other-model" {
		t.Fatal("ProviderConfigs was not deep-copied")
	}
	if orig.Locales[0].EngineCodes["google_web"] == "ja" {
		t.Fatal("Locales EngineCodes was not deep-copied")
	}
	if orig.Profiles[0].LocaleIDs[0] == "xx" {
		t.Fatal("Profiles LocaleIDs was not deep-copied")
	}
}

func TestSaveToAndLoadFromPath(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "config.json")

	// Missing file returns Default without error
	cfg, gotPath, err := loadFromPath(primary)
	if err != nil {
		t.Fatalf("unexpected error when missing: %v", err)
	}
	if gotPath != primary || cfg.Concurrency != 4 {
		t.Fatalf("unexpected default load: path=%q concurrency=%d", gotPath, cfg.Concurrency)
	}

	// Save and verify round-trip load
	saved := Default()
	saved.Concurrency = 12
	saved.AppendContext = "SettingsDialog"
	if err := SaveTo(primary, saved); err != nil {
		t.Fatalf("SaveTo primary error: %v", err)
	}
	loaded, gotPath, err := loadFromPath(primary)
	if err != nil {
		t.Fatalf("loadFromPath primary error: %v", err)
	}
	if gotPath != primary {
		t.Fatalf("expected primary path %q, got %q", primary, gotPath)
	}
	if loaded.Concurrency != 12 || loaded.AppendContext != "SettingsDialog" {
		t.Fatalf("unexpected loaded values: %+v", loaded)
	}

	// Corrupt JSON returns error
	if err := os.WriteFile(primary, []byte("{bad json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadFromPath(primary); err == nil {
		t.Fatal("expected error on corrupt JSON")
	}
}
