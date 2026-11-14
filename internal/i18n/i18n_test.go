package i18n

import (
	"reflect"
	"testing"
)

func TestCatalogCompleteness(t *testing.T) {
	check := func(name string, cat *Catalog) {
		t.Helper()
		v := reflect.ValueOf(cat).Elem()
		typ := v.Type()
		for i := range v.NumField() {
			f := v.Field(i)
			if f.Kind() != reflect.String {
				t.Fatalf("%s.%s has unexpected kind %s", name, typ.Field(i).Name, f.Kind())
			}
			if f.String() == "" {
				t.Errorf("%s.%s must not be empty", name, typ.Field(i).Name)
			}
		}
	}

	check("zhCatalog", &zhCatalog)
	check("enCatalog", &enCatalog)
}

func TestResolveAndOf(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", LangZH},
		{"zh", LangZH},
		{"ZH", LangZH},
		{"zh-CN", LangZH},
		{"zh_TW", LangZH},
		{"en", LangEN},
		{"EN", LangEN},
		{"en-US", LangEN},
		{"en_GB", LangEN},
		{"fr", LangZH},
	}
	for _, tc := range cases {
		if got := Resolve(tc.in); got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if Of("en") != &enCatalog {
		t.Fatal("Of(\"en\") should return &enCatalog")
	}
	if Of("zh") != &zhCatalog {
		t.Fatal("Of(\"zh\") should return &zhCatalog")
	}
}

func TestResolveAutoFromEnv(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := Resolve(LangAuto); got != LangEN {
		t.Fatalf("Resolve(auto) with LANG=en_US.UTF-8 = %q, want en", got)
	}

	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := Resolve(LangAuto); got != LangZH {
		t.Fatalf("Resolve(auto) with LANG=zh_CN.UTF-8 = %q, want zh", got)
	}

	t.Setenv("LANG", "ja_JP.UTF-8")
	if got := Resolve(LangAuto); got != LangEN {
		t.Fatalf("Resolve(auto) with non-Chinese LANG=ja_JP.UTF-8 = %q, want en", got)
	}

	t.Setenv("LANG", "")
	if got := Resolve(LangAuto); got != LangZH {
		t.Fatalf("Resolve(auto) with unset env = %q, want zh", got)
	}
}

func TestResolveAutoWithSystemLocale(t *testing.T) {
	t.Cleanup(func() { SetSystemLocale("") })
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")

	SetSystemLocale("en-US")
	if got := Resolve(LangAuto); got != LangEN {
		t.Fatalf("Resolve(auto) with SystemLocale=en-US = %q, want en", got)
	}

	SetSystemLocale("zh-Hans-CN")
	if got := Resolve(LangAuto); got != LangZH {
		t.Fatalf("Resolve(auto) with SystemLocale=zh-Hans-CN = %q, want zh", got)
	}
}
