package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"v0.1.0", "0.1.0", 0},
		{"0.1.0", "0.1.1", -1},
		{"0.1.0", "0.2.0", -1},
		{"0.1.0", "1.0.0", -1},
		{"0.2.0", "0.1.9", 1},
		{"0.1.1-rc1", "0.1.0", 1},
	}
	for _, tc := range cases {
		if got := CompareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v0.2.0",
			"html_url": "https://github.com/cxxzsh/locloom/releases/tag/v0.2.0",
			"body": "New features",
			"published_at": "2026-11-20T00:00:00Z",
			"draft": false
		}`))
	}))
	defer srv.Close()

	info, err := CheckLatest(context.Background(), srv.Client(), srv.URL, "0.1.0")
	if err != nil {
		t.Fatalf("CheckLatest failed: %v", err)
	}
	if !info.HasUpdate || info.LatestVersion != "0.2.0" {
		t.Fatalf("unexpected release info: %+v", info)
	}
}
