// Package core holds the launcher logic that does not depend on any UI:
// manifest parsing, signature checks, local file scanning, diffing,
// downloading with resume and verification, and applying updates.
package core

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// ManifestFile is one client file the server wants players to have.
type ManifestFile struct {
	Path   string `json:"path"`   // forward slashes, relative to the game folder
	Size   int64  `json:"size"`   // bytes
	SHA256 string `json:"sha256"` // lowercase hex
	// Once: install only when the file is missing (player settings such as allods2.cfg).
	Once bool `json:"once,omitempty"`
}

// LauncherInfo describes the newest launcher exe (for self-update).
type LauncherInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"` // absolute, or relative to the manifest URL
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// Manifest is the server's description of the current client.
type Manifest struct {
	Format   int            `json:"format"`           // 1
	Version  string         `json:"version"`          // client release, e.g. "2026.10.13-1"
	News     string         `json:"news"`             // text shown in the launcher
	Exe      string         `json:"exe"`              // e.g. "allods2.exe"
	Args     []string       `json:"args"`             // e.g. ["-hat","biffinboys.ddns.net"]
	Objects  string         `json:"objects"`          // base URL for files: <objects>/<sha256>
	Delete   []string       `json:"delete,omitempty"` // files to remove (moved to backup)
	Files    []ManifestFile `json:"files"`
	Launcher *LauncherInfo  `json:"launcher,omitempty"`
}

// ParseManifest decodes and validates a manifest. If pub is non-nil the
// detached signature (base64 ed25519 over the exact manifest bytes) must verify.
func ParseManifest(data, sig []byte, pub ed25519.PublicKey) (*Manifest, error) {
	if pub != nil {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
		if err != nil || !ed25519.Verify(pub, data, raw) {
			return nil, errors.New("the update list is not signed by the server owner (signature check failed)")
		}
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("the update list is damaged: %v", err)
	}
	if m.Format != 1 {
		return nil, fmt.Errorf("the update list uses format %d; please download the newest launcher", m.Format)
	}
	if m.Exe == "" {
		return nil, errors.New("the update list does not name the game exe")
	}
	if err := checkRel(m.Exe); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if err := checkRel(f.Path); err != nil {
			return nil, err
		}
		if len(f.SHA256) != 64 || strings.ToLower(f.SHA256) != f.SHA256 {
			return nil, fmt.Errorf("bad checksum for %s in the update list", f.Path)
		}
		k := strings.ToLower(f.Path)
		if seen[k] {
			return nil, fmt.Errorf("%s is listed twice in the update list", f.Path)
		}
		seen[k] = true
	}
	for _, d := range m.Delete {
		if err := checkRel(d); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// checkRel rejects absolute paths and anything escaping the game folder.
func checkRel(p string) error {
	if p == "" || strings.Contains(p, "\\") || strings.Contains(p, ":") || strings.HasPrefix(p, "/") {
		return fmt.Errorf("unsafe file path in the update list: %q", p)
	}
	c := path.Clean(p)
	if c != p || c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return fmt.Errorf("unsafe file path in the update list: %q", p)
	}
	if strings.HasPrefix(strings.ToLower(c), ".a2launcher") {
		return fmt.Errorf("reserved path in the update list: %q", p)
	}
	return nil
}

// Resolve makes ref absolute against the manifest URL.
func Resolve(manifestURL, ref string) (string, error) {
	b, err := url.Parse(manifestURL)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(r).String(), nil
}

// ObjectURL is where the file with this hash is downloaded from.
func (m *Manifest) ObjectURL(manifestURL, sha string) (string, error) {
	base := m.Objects
	if base == "" {
		base = "objects/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return Resolve(manifestURL, base+sha)
}
