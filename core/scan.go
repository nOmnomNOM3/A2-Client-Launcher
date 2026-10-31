package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// StateDir is the launcher's private folder inside the game folder.
const StateDir = ".a2launcher"

type cacheEntry struct {
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
	Hash  string `json:"sha256"`
}

// HashCache remembers file hashes by size+mtime so a normal launch does not
// re-read hundreds of MB. Repair ignores it.
type HashCache struct {
	path    string
	Entries map[string]cacheEntry `json:"files"`
}

func LoadHashCache(root string) *HashCache {
	c := &HashCache{path: filepath.Join(root, StateDir, "hashes.json"), Entries: map[string]cacheEntry{}}
	if b, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(b, c)
		if c.Entries == nil {
			c.Entries = map[string]cacheEntry{}
		}
	}
	return c
}

func (c *HashCache) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(c)
	return os.WriteFile(c.path, b, 0o644)
}

// HashFile returns the lowercase hex SHA-256 of a file.
func HashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// LocalHash hashes rel under root, using the cache unless full is set.
// Returns "" with nil error when the file does not exist.
func (c *HashCache) LocalHash(root, rel string, full bool) (string, int64, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	st, err := os.Stat(p)
	if os.IsNotExist(err) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	key := strings.ToLower(rel)
	if e, ok := c.Entries[key]; ok && !full && e.Size == st.Size() && e.MTime == st.ModTime().UnixNano() {
		return e.Hash, st.Size(), nil
	}
	h, err := HashFile(p)
	if err != nil {
		return "", 0, err
	}
	c.Entries[key] = cacheEntry{Size: st.Size(), MTime: st.ModTime().UnixNano(), Hash: h}
	return h, st.Size(), nil
}

// Plan is the work needed to bring a folder up to date.
type Plan struct {
	Fetch      []ManifestFile // missing or different
	Delete     []string       // present files the manifest wants removed
	TotalBytes int64
	Checked    int
}

// Diff compares the folder against the manifest. progress(done,total) may be nil.
func Diff(root string, m *Manifest, cache *HashCache, full bool, progress func(done, total int, name string)) (*Plan, error) {
	p := &Plan{}
	for i, f := range m.Files {
		if progress != nil {
			progress(i, len(m.Files), f.Path)
		}
		if IsCharCacheFile(f.Path) {
			continue // client character cache: never restored from the manifest
		}
		if f.Once {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.Path))); err == nil {
				p.Checked++
				continue
			}
		}
		h, size, err := cache.LocalHash(root, f.Path, full)
		if err != nil {
			return nil, err
		}
		p.Checked++
		if h != f.SHA256 || size != f.Size {
			p.Fetch = append(p.Fetch, f)
			p.TotalBytes += f.Size
		}
	}
	for _, d := range m.Delete {
		if IsCharCacheFile(d) {
			continue // ClearCharCache handles these at launch
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(d))); err == nil {
			p.Delete = append(p.Delete, d)
		}
	}
	if progress != nil {
		progress(len(m.Files), len(m.Files), "")
	}
	return p, nil
}
