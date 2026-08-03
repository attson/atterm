package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// githubReleaseAPI returns the URL to fetch the latest release manifest.
func (u *Updater) githubReleaseAPI() string {
	if u.cfg.releaseURL != "" {
		return u.cfg.releaseURL
	}
	return "https://api.github.com/repos/" + u.cfg.repo + "/releases/latest"
}

// githubReleasesAPI returns the URL to fetch the full releases list.
func (u *Updater) githubReleasesAPI() string {
	if u.cfg.releasesURL != "" {
		return u.cfg.releasesURL
	}
	return "https://api.github.com/repos/" + u.cfg.repo + "/releases"
}

// githubLatestURL returns the browser endpoint whose redirect target carries
// the latest tag without consuming GitHub's unauthenticated API quota.
func (u *Updater) githubLatestURL() string {
	if u.cfg.latestURL != "" {
		return u.cfg.latestURL
	}
	return "https://github.com/" + u.cfg.repo + "/releases/latest"
}

// githubAsset is the subset of the release-asset JSON we care about.
type githubAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
}

// githubRelease is the subset of the release JSON we care about.
type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Body       string        `json:"body"`
	Prerelease bool          `json:"prerelease"`
	Draft      bool          `json:"draft"`
	Assets     []githubAsset `json:"assets"`
}

// Check fetches the latest release. force=true bypasses the 1h response cache.
// In dev/empty builds it's a no-op that never touches the network.

func (u *Updater) tryClaimExistingArchive(ctx context.Context, latest string) (bool, error) {
	dir, err := u.updatesDir()
	if err != nil {
		return false, err
	}
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false, err
	}
	final := filepath.Join(dir, latest+"-"+name)
	if _, err := os.Stat(final); err != nil {
		return false, nil // not on disk — miss
	}
	if verifyErr := u.verifyDownloadedArchive(ctx, final, name); verifyErr != nil {
		_ = os.Remove(final) // corrupted; treat as miss
		return false, nil
	}
	u.mu.Lock()
	u.state.Downloading = false
	u.state.Ready = true
	u.state.DownloadPct = 100
	u.state.DownloadDir = dir
	u.state.DownloadPath = final
	u.state.DownloadedExists = true
	u.state.Error = ""
	u.mu.Unlock()
	return true, nil
}

func (u *Updater) fetchLatest(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.githubReleaseAPI(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AT-Term/"+u.cfg.current)
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == http.StatusForbidden || u.cfg.latestURL != "" {
			if rel, ferr := u.fetchLatestViaRedirect(ctx); ferr == nil {
				return rel, nil
			}
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return nil, fmt.Errorf("github returned http %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("github returned http %d: %s", resp.StatusCode, detail)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// fetchReleases fetches the full releases list from GitHub. Mirrors
// fetchLatest's header/decode shape, decoding into a slice.
func (u *Updater) fetchReleases(ctx context.Context) ([]githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.githubReleasesAPI(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AT-Term/"+u.cfg.current)
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github releases list http %d", resp.StatusCode)
	}
	var rels []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, err
	}
	return rels, nil
}

// refreshLines fetches the releases list and groups it into version lines.
// It only reads u.cfg and issues HTTP — it MUST NOT be called while holding
// u.mu (it would hold the lock across a slow network call). Returns nil on
// any failure so callers can degrade gracefully without disturbing the
// existing latest-release state.
func (u *Updater) refreshLines(ctx context.Context) []VersionLine {
	rels, err := u.fetchReleases(ctx)
	if err != nil {
		log.Printf("updater: fetch releases list: %v", err)
		return nil
	}
	assetName, perr := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if perr != nil {
		return nil
	}
	var cands []lineCandidate
	for _, rel := range rels {
		if rel.Prerelease || rel.Draft {
			continue
		}
		var assetURL string
		for _, a := range rel.Assets {
			if a.Name == assetName {
				assetURL = a.DownloadURL
				break
			}
		}
		if assetURL == "" {
			continue
		}
		cands = append(cands, lineCandidate{tag: rel.TagName, assetURL: assetURL, notes: rel.Body})
	}
	return groupLines(cands, u.cfg.current)
}


func (u *Updater) fetchLatestViaRedirect(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", u.githubLatestURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AT-Term/"+u.cfg.current)
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github latest redirect returned http %d", resp.StatusCode)
	}
	final := resp.Request.URL
	tag, err := tagFromReleaseURL(final)
	if err != nil {
		return nil, err
	}
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	return &githubRelease{
		TagName: tag,
		Assets: []githubAsset{
			{
				Name:        name,
				DownloadURL: assetDownloadURL(final, tag, name),
			},
			{
				Name:        "SHA256SUMS",
				DownloadURL: assetDownloadURL(final, tag, "SHA256SUMS"),
			},
			{
				Name:        "SHA256SUMS.sig",
				DownloadURL: assetDownloadURL(final, tag, "SHA256SUMS.sig"),
			},
		},
	}, nil
}

func tagFromReleaseURL(u *url.URL) (string, error) {
	const marker = "/releases/tag/"
	idx := strings.LastIndex(u.Path, marker)
	if idx < 0 {
		return "", fmt.Errorf("github latest redirect target %q has no release tag", u.String())
	}
	tag, err := url.PathUnescape(strings.Trim(u.Path[idx+len(marker):], "/"))
	if err != nil {
		return "", err
	}
	if tag == "" {
		return "", fmt.Errorf("github latest redirect target %q has empty release tag", u.String())
	}
	return tag, nil
}

func assetDownloadURL(final *url.URL, tag, name string) string {
	const marker = "/releases/tag/"
	idx := strings.LastIndex(final.Path, marker)
	prefix := ""
	if idx >= 0 {
		prefix = final.Path[:idx]
	}
	out := *final
	out.Path = prefix + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
	out.RawPath = ""
	out.RawQuery = ""
	out.Fragment = ""
	return out.String()
}

func proxiedGitHubReleaseURL(rawURL, proxyBase string) string {
	proxyBase = strings.TrimSpace(proxyBase)
	if proxyBase == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || strings.ToLower(u.Host) != "github.com" || !strings.Contains(u.Path, "/releases/download/") {
		return rawURL
	}
	return strings.TrimRight(proxyBase, "/") + "/" + rawURL
}

