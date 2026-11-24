package providers

import (
	"context"
	"testing"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

func TestPatientViewsLive(t *testing.T) {
	items := []provider.SourceItem{
		{ID: "a", Key: domain.EntryKey{Context: "M", Source: "Patient Views:"}, SrcText: "Patient Views:", RefLocale: "en_US"},
		{ID: "b", Key: domain.EntryKey{Context: "M", Source: "Patient Views: %1"}, SrcText: "Patient Views: %1", RefLocale: "en_US"},
		{ID: "c", Key: domain.EntryKey{Context: "M", Source: "Exit"}, SrcText: "Exit", RefLocale: "en_US"},
	}
	p := NewGoogleWebProvider()
	if err := p.Configure(map[string]string{"use_proxy": "false"}, netpool.New(netpool.ProxyConfig{})); err != nil {
		t.Fatal(err)
	}
	out, err := p.Translate(context.Background(), provider.Request{
		TargetLocale: locale.LocaleSpec{ID: "ja_JP"}, EngineCode: "ja", Items: items,
	})
	t.Logf("err=%v out=%#v", err, out)
	for _, it := range items {
		m := provider.MaskPlaceholders(it.SrcText)
		t.Logf("%q -> %q", it.SrcText, provider.RestorePlaceholders(out[it.ID], m))
	}
}
