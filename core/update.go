package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Progress receives status for the UI.
type Progress interface {
	Status(text string)
	Bytes(done, total int64)
}

// Apply downloads everything in the plan, verifies it, backs up replaced
// files, then moves new files into place. Nothing in the game folder changes
// until every download is verified.
func Apply(ctx context.Context, root, manifestURL string, m *Manifest, p *Plan, cache *HashCache, pr Progress) error {
	state := filepath.Join(root, StateDir)
	tmp := filepath.Join(state, "download")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("cannot write to the game folder %s: %v", root, err)
	}
	var done int64
	for i, f := range p.Fetch {
		pr.Status(fmt.Sprintf("Downloading %s (%d of %d)", f.Path, i+1, len(p.Fetch)))
		u, err := m.ObjectURL(manifestURL, f.SHA256)
		if err != nil {
			return err
		}
		start := done
		err = DownloadVerified(ctx, u, filepath.Join(tmp, f.SHA256+".part"), f.Size, f.SHA256, func(n int64) {
			done += n
			pr.Bytes(done, p.TotalBytes)
		})
		if err != nil {
			return fmt.Errorf("downloading %s failed: %w", f.Path, err)
		}
		done = start + f.Size
		pr.Bytes(done, p.TotalBytes)
	}
	if len(p.Fetch) == 0 && len(p.Delete) == 0 {
		return nil
	}
	pr.Status("Installing files")
	backup := filepath.Join(state, "backup", time.Now().Format("20060102-150405"))
	move := func(rel string) error {
		src := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(src); err != nil {
			return nil
		}
		dst := filepath.Join(backup, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("cannot replace %s; close the game and try again (%v)", rel, err)
		}
		return nil
	}
	for _, f := range p.Fetch {
		if err := move(f.Path); err != nil {
			return err
		}
		dst := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(tmp, f.SHA256+".part"), dst); err != nil {
			// same object may be used by two paths: copy from the first
			if err2 := copyFile(findInstalled(root, p, f), dst); err2 != nil {
				return fmt.Errorf("cannot install %s: %v", f.Path, err)
			}
		}
		cache.LocalHash(root, f.Path, true)
	}
	for _, d := range p.Delete {
		if err := move(d); err != nil {
			return err
		}
	}
	cache.Save()
	PruneBackups(filepath.Join(state, "backup"), 3)
	return nil
}

func findInstalled(root string, p *Plan, f ManifestFile) string {
	for _, o := range p.Fetch {
		if o.SHA256 == f.SHA256 && o.Path != f.Path {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(o.Path))); err == nil {
				return filepath.Join(root, filepath.FromSlash(o.Path))
			}
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	if src == "" {
		return fmt.Errorf("no source")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// PruneBackups keeps only the newest keep backup folders.
func PruneBackups(dir string, keep int) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		os.RemoveAll(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}
