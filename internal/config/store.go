package config

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/platform"
)

const configFileName = "config.json"

// AppConfig persists user preferences, proxy configuration, dynamic locales, and per-provider settings.
type AppConfig struct {
	UILang           string                       `json:"ui_lang"`
	ThemeMode        int                          `json:"theme_mode"`
	LeftPaneWidth    float32                      `json:"left_pane_width"`
	BottomPaneHeight float32                      `json:"bottom_pane_height"`
	MatrixViewMode   int                          `json:"matrix_view_mode"`
	FilePattern      string                       `json:"file_pattern"`
	WriteMode        domain.WriteMode             `json:"write_mode"`
	AppendMissing    bool                         `json:"append_missing"`
	AppendContext    string                       `json:"append_context"`
	CreateBackup     bool                         `json:"create_backup"`
	EnablePluralForms bool                        `json:"enable_plural_forms"`
	BypassCache      bool                         `json:"bypass_cache"`
	Concurrency      int                          `json:"concurrency"`
	LastTSPath       string                       `json:"last_ts_path,omitempty"`
	LastXLSXPath     string                       `json:"last_xlsx_path,omitempty"`
	Proxy            netpool.ProxyConfig          `json:"proxy"`
	ActiveProviders  []string                     `json:"active_providers"`
	ProviderOrder    []string                     `json:"provider_order"`
	ProviderConfigs  map[string]map[string]string `json:"provider_configs"`
	Locales          []locale.LocaleSpec          `json:"locales"`
	Profiles         []locale.Profile             `json:"profiles"`
}

// Default returns a populated AppConfig with clean, company-neutral defaults.
func Default() AppConfig {
	return AppConfig{
		UILang:           "zh",
		ThemeMode:        0,
		LeftPaneWidth:    340,
		BottomPaneHeight: 230,
		MatrixViewMode:   0,
		FilePattern:      "{id}.ts",
		WriteMode:        domain.ModeFillUnfinished,
		AppendMissing:    false,
		AppendContext:    "MainWindow",
		CreateBackup:     true,
		BypassCache:      false,
		Concurrency:      4,
		Proxy: netpool.ProxyConfig{
			ProxyURL:      "",
			SkipTLSVerify: false,
			Timeout:       60 * time.Second,
		},
		ActiveProviders: []string{"xlsx", "ts_tm", "google_web"},
		ProviderOrder: []string{
			"xlsx",
			"ts_tm",
			"google_web",
			"bing_web",
			"libretranslate",
			"mymemory",
			"deepl",
			"openai_compat",
			"baidu_api",
			"niutrans",
		},
		ProviderConfigs: map[string]map[string]string{
			"xlsx": {
				"file_path": "",
				"sheet":     "",
				"skip_ref":  "true",
			},
			"ts_tm": {
				"tm_path": "",
			},
			"google_web": {
				"host":       "translate.googleapis.com",
				"endpoint":   "gtx",
				"batch_size": "25",
				"use_proxy":  "false",
			},
			"bing_web": {
				"mode":         "edge_free",
				"endpoint_url": "https://edge.microsoft.com/translate/translatetext",
				"api_key":      "",
				"region":       "",
				"batch_size":   "25",
				"use_proxy":    "false",
			},
			"deepl": {
				"mode":       "deeplx",
				"base_url":   "http://127.0.0.1:1188/translate",
				"api_key":    "",
				"batch_size": "25",
				"use_proxy":  "false",
			},
			"mymemory": {
				"base_url":    "https://api.mymemory.translated.net",
				"source_lang": "en",
				"email":       "",
				"api_key":     "",
				"use_proxy":   "false",
			},
			"libretranslate": {
				"mode":       "lingva",
				"base_url":   "https://lingva.ml",
				"api_key":    "",
				"batch_size": "20",
				"use_proxy":  "false",
			},
			"baidu_api": {
				"app_id":       "",
				"app_secret":   "",
				"base_url":     "https://fanyi-api.baidu.com/api/trans/vip/translate",
				"qps_delay_ms": "200",
				"use_proxy":    "false",
			},
			"niutrans": {
				"api_key":     "",
				"source_lang": "auto",
				"base_url":    "https://api.niutrans.com/NiuTransServer/translation",
				"use_proxy":   "false",
			},
			"openai_compat": {
				"base_url":      "https://api.openai.com/v1",
				"model":         "gpt-4o-mini",
				"api_key":       "",
				"domain_prompt": "",
				"use_proxy":     "false",
			},
		},
		Locales:  locale.DefaultLocales(),
		Profiles: locale.DefaultProfiles(),
	}
}

// Clone returns a deep copy of AppConfig so background saves never race with UI mutations.
func (c AppConfig) Clone() AppConfig {
	out := c

	if c.ActiveProviders != nil {
		out.ActiveProviders = slices.Clone(c.ActiveProviders)
	}
	if c.ProviderOrder != nil {
		out.ProviderOrder = slices.Clone(c.ProviderOrder)
	}
	if c.ProviderConfigs != nil {
		out.ProviderConfigs = make(map[string]map[string]string, len(c.ProviderConfigs))
		for pid, kv := range c.ProviderConfigs {
			inner := make(map[string]string, len(kv))
			for k, v := range kv {
				inner[k] = v
			}
			out.ProviderConfigs[pid] = inner
		}
	}
	if c.Locales != nil {
		out.Locales = make([]locale.LocaleSpec, len(c.Locales))
		for i, loc := range c.Locales {
			out.Locales[i] = loc.Clone()
		}
	}
	if c.Profiles != nil {
		out.Profiles = make([]locale.Profile, len(c.Profiles))
		for i, prof := range c.Profiles {
			out.Profiles[i] = locale.Profile{
				ID:          prof.ID,
				Name:        prof.Name,
				EnglishName: prof.EnglishName,
				LocaleIDs:   slices.Clone(prof.LocaleIDs),
			}
		}
	}

	return out
}

// Load reads config.json or returns Default() when the file does not exist yet.
func Load() (AppConfig, string, error) {
	path := platform.ResolveConfigPath(configFileName)
	return loadFromPath(path)
}

func loadFromPath(primaryPath string) (AppConfig, string, error) {
	cfg := Default()

	raw, err := os.ReadFile(primaryPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return cfg, primaryPath, err
		}
		return cfg, primaryPath, nil
	}

	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Default(), primaryPath, err
	}

	normalizeLoaded(&cfg)
	return cfg, primaryPath, nil
}

func normalizeLoaded(cfg *AppConfig) {
	def := Default()
	cfg.UILang = strings.ToLower(strings.TrimSpace(cfg.UILang))
	if cfg.UILang != "zh" && cfg.UILang != "en" && cfg.UILang != "auto" {
		cfg.UILang = def.UILang
	}
	if cfg.LeftPaneWidth <= 0 {
		cfg.LeftPaneWidth = def.LeftPaneWidth
	}
	if cfg.BottomPaneHeight <= 0 {
		cfg.BottomPaneHeight = def.BottomPaneHeight
	}
	if cfg.FilePattern == "" {
		cfg.FilePattern = def.FilePattern
	}
	if cfg.AppendContext == "" {
		cfg.AppendContext = def.AppendContext
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = def.Concurrency
	}
	if cfg.Proxy.Timeout <= 0 {
		cfg.Proxy.Timeout = def.Proxy.Timeout
	}
	if len(cfg.Locales) == 0 {
		cfg.Locales = def.Locales
	}
	if len(cfg.Profiles) == 0 {
		cfg.Profiles = def.Profiles
	}
	if len(cfg.ProviderOrder) == 0 || isLegacyDefaultProviderOrder(cfg.ProviderOrder) {
		cfg.ProviderOrder = slices.Clone(def.ProviderOrder)
	} else {
		for _, pid := range def.ProviderOrder {
			if !slices.Contains(cfg.ProviderOrder, pid) {
				cfg.ProviderOrder = append(cfg.ProviderOrder, pid)
			}
		}
	}
	if cfg.ProviderConfigs == nil {
		cfg.ProviderConfigs = def.ProviderConfigs
	} else {
		for pid, defKV := range def.ProviderConfigs {
			if cfg.ProviderConfigs[pid] == nil {
				cfg.ProviderConfigs[pid] = make(map[string]string, len(defKV))
			}
			for k, v := range defKV {
				if _, exists := cfg.ProviderConfigs[pid][k]; !exists {
					cfg.ProviderConfigs[pid][k] = v
				}
			}
		}
	}
}

func isLegacyDefaultProviderOrder(order []string) bool {
	legacyDefaults := [][]string{
		{"xlsx", "ts_tm", "google_web", "openai_compat"},
		{"xlsx", "ts_tm", "google_web", "openai_compat", "bing_web", "deepl", "mymemory", "libretranslate", "baidu_api", "niutrans"},
		{"xlsx", "ts_tm", "google_web", "bing_web", "deepl", "mymemory", "libretranslate", "baidu_api", "niutrans", "openai_compat"},
	}
	for _, legacy := range legacyDefaults {
		if slices.Equal(order, legacy) {
			return true
		}
	}
	return false
}

// Save writes cfg atomically to the resolved config.json path.
func Save(cfg AppConfig) error {
	return SaveTo(platform.ResolveConfigPath(configFileName), cfg)
}

// SaveTo writes cfg atomically to path.
func SaveTo(path string, cfg AppConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0600)
}
