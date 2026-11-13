package providers

import "locloom/internal/provider"

func init() {
	RegisterBuiltins(provider.DefaultRegistry())
}

// RegisterBuiltins registers all built-in local and engine translation providers into reg.
func RegisterBuiltins(reg *provider.Registry) {
	if reg == nil {
		return
	}
	reg.Register(NewXLSXProvider())
	reg.Register(NewTSTMProvider())
	reg.Register(NewGoogleWebProvider())
	reg.Register(NewBingWebProvider())
	reg.Register(NewDeepLProvider())
	reg.Register(NewMyMemoryProvider())
	reg.Register(NewLibreTranslateProvider())
	reg.Register(NewBaiduProvider())
	reg.Register(NewNiutransProvider())
	reg.Register(NewOpenAICompatProvider())
}
