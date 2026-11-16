package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultLatestReleaseURL = "https://api.github.com/repos/cxxzsh/locloom/releases/latest"

// ReleaseInfo describes the latest release metadata returned by GitHub Releases.
type ReleaseInfo struct {
	CurrentVersion string
	LatestVersion  string
	TagName        string
	HTMLURL        string
	Notes          string
	PublishedAt    string
	HasUpdate      bool
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

// CheckLatest queries the GitHub Releases API (or a custom endpoint) and compares it against currentVersion.
func CheckLatest(ctx context.Context, client *http.Client, endpoint, currentVersion string) (ReleaseInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultLatestReleaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "LocLoom/"+strings.TrimPrefix(strings.TrimSpace(currentVersion), "v"))

	resp, err := client.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return ReleaseInfo{}, err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	cur := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")
	hasUpdate := !rel.Draft && latest != "" && CompareSemver(cur, latest) < 0

	return ReleaseInfo{
		CurrentVersion: cur,
		LatestVersion:  latest,
		TagName:        rel.TagName,
		HTMLURL:        rel.HTMLURL,
		Notes:          strings.TrimSpace(rel.Body),
		PublishedAt:    rel.PublishedAt,
		HasUpdate:      hasUpdate,
	}, nil
}

// CompareSemver compares two semantic version strings (e.g. "0.1.0" vs "0.1.1").
// Returns -1 if a < b, 0 if a == b, and +1 if a > b.
func CompareSemver(a, b string) int {
	pa := parseSemverParts(a)
	pb := parseSemverParts(b)
	for i := range 3 {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parseSemverParts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	segs := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < 3 && i < len(segs); i++ {
		n, err := strconv.Atoi(strings.TrimSpace(segs[i]))
		if err == nil && n >= 0 {
			out[i] = n
		}
	}
	return out
}
