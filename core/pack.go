package core

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Workers is how many files download at once.
var Workers = 12

// Pack thresholds: use a pack when more than PackMinFraction of its files are
// needed, or more than PackMinFiles of them.
var (
	PackMinFraction = 0.40
	PackMinFiles    = 200
)

// choosePacks picks packs worth downloading for the files still needed.
// need maps sha256 -> true for files in the plan.
func choosePacks(m *Manifest, fetch []ManifestFile) []Pack {
	byPath := map[string]ManifestFile{}
	for _, f := range fetch {
		byPath[f.Path] = f
	}
	var out []Pack
	for _, pk := range m.Packs {
		if len(pk.Files) == 0 {
			continue
		}
		n := 0
		for _, p := range pk.Files {
			if _, ok := byPath[p]; ok {
				n++
			}
		}
		if n > PackMinFiles || float64(n) > PackMinFraction*float64(len(pk.Files)) {
			out = append(out, pk)
		}
	}
	return out
}

// extractPack pulls the wanted files (by manifest path) out of a verified
// pack zip into tmp/<sha256>.part, checking each file's size and hash.
// Returns the set of paths it produced.
func extractPack(zipPath, tmp string, want map[string]ManifestFile) (map[string]bool, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	got := map[string]bool{}
	for _, zf := range zr.File {
		if err := checkRel(zf.Name); err != nil {
			return nil, fmt.Errorf("pack: %v", err)
		}
		f, ok := want[zf.Name]
		if !ok || got[f.Path] {
			continue
		}
		if int64(zf.UncompressedSize64) != f.Size {
			return nil, fmt.Errorf("pack: %s has the wrong size", f.Path)
		}
		dst := filepath.Join(tmp, f.SHA256+".part")
		if err := extractOne(zf, dst, f); err != nil {
			os.Remove(dst)
			return nil, err
		}
		got[f.Path] = true
	}
	return got, nil
}

func extractOne(zf *zip.File, dst string, f ManifestFile) error {
	r, err := zf.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(r, f.Size+1))
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("pack: %s: %v", f.Path, err)
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("pack: %s failed its checksum", f.Path)
	}
	return nil
}

// fetchAll downloads the plan's files into tmp, using packs where worthwhile
// and parallel per-file downloads for the rest.
func fetchAll(ctx context.Context, tmp, manifestURL string, m *Manifest, fetch []ManifestFile, pr Progress) error {
	packs := choosePacks(m, fetch)
	var total int64
	for _, pk := range packs {
		total += pk.Size
	}
	for _, f := range fetch {
		total += f.Size
	}
	var done int64
	var mu sync.Mutex
	report := func(n int64) {
		mu.Lock()
		pr.Bytes(atomic.AddInt64(&done, n), atomic.LoadInt64(&total))
		mu.Unlock()
	}
	setStatus := func(s string) { mu.Lock(); pr.Status(s); mu.Unlock() }

	have := map[string]bool{} // paths already in tmp, verified
	covered := map[string]bool{}
	for i, pk := range packs {
		want := map[string]ManifestFile{}
		for _, f := range fetch {
			want[f.Path] = f
		}
		setStatus(fmt.Sprintf("Downloading pack %d of %d", i+1, len(packs)))
		got, err := fetchPack(ctx, tmp, manifestURL, m, pk, want, func(n int64) { report(n) })
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// fall back to per-file for these files
			setStatus("Pack failed; downloading files one by one")
		}
		for p := range got {
			have[p] = true
		}
		// pack bytes counted in total already; the covered files' sizes are not needed
		var skip int64
		for _, f := range fetch {
			if got[f.Path] && !covered[f.Path] {
				skip += f.Size
				covered[f.Path] = true
			}
		}
		atomic.AddInt64(&total, -skip)
		pr.Bytes(atomic.LoadInt64(&done), atomic.LoadInt64(&total))
	}

	var rest []ManifestFile
	seenSha := map[string]bool{}
	for _, f := range fetch {
		if have[f.Path] {
			seenSha[f.SHA256] = true
			continue
		}
		if seenSha[f.SHA256] { // same content already downloaded/queued
			atomic.AddInt64(&total, -f.Size)
			continue
		}
		seenSha[f.SHA256] = true
		rest = append(rest, f)
	}
	workers := Workers
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	var firstErr error
	var errOnce sync.Once
	var finished int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				f := rest[i]
				u, err := m.ObjectURL(manifestURL, f.SHA256)
				var counted int64
				if err == nil {
					err = DownloadVerified(ctx, u, filepath.Join(tmp, f.SHA256+".part"), f.Size, f.SHA256, func(n int64) {
						counted += n
						report(n)
					})
				}
				if err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("downloading %s failed: %w", f.Path, err); cancel() })
					continue
				}
				report(f.Size - counted) // fix up retries / resumed bytes
				k := atomic.AddInt64(&finished, 1)
				setStatus(fmt.Sprintf("Downloading files (%d of %d)", k, len(rest)))
			}
		}()
	}
	for i := range rest {
		select {
		case jobs <- i:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
	return firstErr
}

func fetchPack(ctx context.Context, tmp, manifestURL string, m *Manifest, pk Pack, want map[string]ManifestFile, onBytes func(int64)) (map[string]bool, error) {
	u, err := m.ObjectURL(manifestURL, pk.SHA256)
	if err != nil {
		return nil, err
	}
	zp := filepath.Join(tmp, "pack-"+pk.SHA256+".zip")
	defer os.Remove(zp)
	var counted int64
	if err := DownloadVerified(ctx, u, zp, pk.Size, pk.SHA256, func(n int64) { counted += n; onBytes(n) }); err != nil {
		return nil, err
	}
	onBytes(pk.Size - counted)
	return extractPack(zp, tmp, want)
}
