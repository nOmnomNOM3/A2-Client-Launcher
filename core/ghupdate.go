package core

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GitHub release self-update. Every launcher checks the newest GitHub release
// at startup; if its tag is a newer version it downloads the RoM2.Launcher.exe
// asset, verifies it, swaps it in and restarts.
//
// Integrity, strongest first:
//   - RoM2.Launcher.exe.sig: base64 ed25519 signature (same key as the
//     manifest) over LauncherSigMessage(version, sha256). Used whenever the
//     launcher has a public key and the release has a .sig asset; if it does
//     not verify, the update is refused.
//   - Otherwise, unless the launcher was built with RequireSig, the SHA-256
//     GitHub reports for the asset ("digest"), or a RoM2.Launcher.exe.sha256
//     asset. This guards against corruption and tampering in transit, but
//     anyone who can publish a release on the repo can publish a matching hash.

// LauncherAsset is the release asset name of the launcher exe.
const LauncherAsset = "RoM2.Launcher.exe"

// GitHubLatestURL is the GitHub API endpoint for the newest release.
var GitHubLatestURL = "https://api.github.com/repos/nOmnomNOM3/A2-Client-Launcher/releases/latest"

// GHRelease is the part of the GitHub release API response we use.
type GHRelease struct {
	TagName    string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []GHAsset `json:"assets"`
}

// GHAsset is one release file.
type GHAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // "sha256:<hex>" (GitHub fills this in for new uploads)
	URL    string `json:"browser_download_url"`
}

func (r *GHRelease) asset(name string) *GHAsset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// LauncherSigMessage is the exact byte string the release workflow signs.
// Binding the version stops an old signed exe being replayed as a "newer" one.
func LauncherSigMessage(version, sha256hex string) []byte {
	return []byte("a2launcher-selfupdate\n" + strings.TrimPrefix(version, "v") + "\n" + strings.ToLower(sha256hex) + "\n")
}

// GHUpdate configures GitHubSelfUpdate.
type GHUpdate struct {
	URL        string            // API URL; default GitHubLatestURL
	Current    string            // this launcher's version
	PubKey     ed25519.PublicKey // nil: signatures cannot be checked
	RequireSig bool              // refuse updates without a valid .sig
	SkipTag    string            // tag we just updated to (loop guard)
	Timeout    time.Duration     // API request timeout; default 5s
	Log        func(format string, a ...any)
	Status     func(string)
}

func (o *GHUpdate) logf(format string, a ...any) {
	if o.Log != nil {
		o.Log(format, a...)
	}
}

// FetchLatestRelease asks GitHub for the newest published release.
func FetchLatestRelease(ctx context.Context, url string, timeout time.Duration) (*GHRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var r GHRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("bad GitHub release data: %v", err)
	}
	return &r, nil
}

// GitHubSelfUpdate replaces exe with the newest release's launcher when it is
// newer than o.Current. It returns the new tag when the caller should restart
// exe, or "" when nothing changed. Skips (not errors) are logged.
func GitHubSelfUpdate(ctx context.Context, exe string, o GHUpdate) (string, error) {
	if IsDevVersion(o.Current) {
		o.logf("self-update: dev build %q, not checking GitHub", o.Current)
		return "", nil
	}
	if o.URL == "" {
		o.URL = GitHubLatestURL
	}
	if o.Timeout == 0 {
		o.Timeout = 5 * time.Second
	}
	rel, err := FetchLatestRelease(ctx, o.URL, o.Timeout)
	if err != nil {
		return "", fmt.Errorf("self-update check failed: %v", err)
	}
	tag := rel.TagName
	if rel.Draft || rel.Prerelease || !NewerVersion(tag, o.Current) {
		o.logf("self-update: launcher %s is current (latest release %s)", o.Current, tag)
		return "", nil
	}
	if o.SkipTag != "" && o.SkipTag == tag {
		o.logf("self-update: just updated to %s but still report %s; not retrying", tag, o.Current)
		return "", nil
	}
	a := rel.asset(LauncherAsset)
	if a == nil || a.Size <= 0 {
		o.logf("self-update: release %s has no %s yet", tag, LauncherAsset)
		return "", nil
	}
	dir := filepath.Dir(exe)
	if err := CanWriteDir(dir); err != nil {
		o.logf("self-update: %s is newer but %s is not writable (Program Files?); skipping", tag, dir)
		return "", nil
	}

	var sig []byte
	if sa := rel.asset(LauncherAsset + ".sig"); sa != nil && o.PubKey != nil {
		b, err := FetchSmall(ctx, sa.URL, 4096)
		if err != nil {
			return "", fmt.Errorf("self-update: cannot fetch signature: %v", err)
		}
		if sig, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(b))); err != nil || len(sig) != ed25519.SignatureSize {
			return "", errors.New("self-update: signature asset is damaged")
		}
	} else if o.RequireSig {
		o.logf("self-update: release %s has no valid signature asset; this launcher requires one, skipping", tag)
		return "", nil
	}
	want := ""
	if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
		want = strings.ToLower(d)
	} else if ha := rel.asset(LauncherAsset + ".sha256"); ha != nil {
		b, err := FetchSmall(ctx, ha.URL, 4096)
		if err == nil {
			if f := strings.Fields(string(b)); len(f) > 0 {
				want = strings.ToLower(f[0])
			}
		}
	}
	if sig == nil && len(want) != 64 {
		o.logf("self-update: release %s has neither a signature nor a SHA-256; skipping", tag)
		return "", nil
	}

	if o.Status != nil {
		o.Status("Updating launcher…")
	}
	o.logf("self-update: %s -> %s (verify: %s)", o.Current, tag, map[bool]string{true: "ed25519 signature", false: "sha256 " + want}[sig != nil])
	newPath := exe + ".new"
	os.Remove(newPath)
	got, err := downloadAll(ctx, a.URL, newPath, a.Size)
	if err == nil && want != "" && got != want {
		err = fmt.Errorf("downloaded launcher failed its checksum (got %s, want %s)", got, want)
	}
	if err == nil && sig != nil && !ed25519.Verify(o.PubKey, LauncherSigMessage(tag, got), sig) {
		err = errors.New("downloaded launcher is not signed by the server owner")
	}
	if err != nil {
		os.Remove(newPath)
		return "", fmt.Errorf("self-update: %v", err)
	}
	if err := SwapExe(exe, newPath); err != nil {
		os.Remove(newPath)
		return "", fmt.Errorf("self-update: cannot replace the launcher: %v", err)
	}
	o.logf("self-update: installed %s, restarting", tag)
	return tag, nil
}

// downloadAll fetches url into dst (exactly size bytes) and returns its SHA-256.
func downloadAll(ctx context.Context, url, dst string, size int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download answered %s", resp.Status)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if n != size {
		return "", fmt.Errorf("downloaded %d bytes, expected %d", n, size)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
