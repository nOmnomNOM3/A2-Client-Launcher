package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// semver is a parsed version: major.minor.patch[-pre][+build].
type semver struct {
	n   [3]int
	pre []string
}

// parseSemver accepts "1.3.0", "v1.3.1", "1.3", "1.3.1-rc.1", "1.3.1+abc".
// "0.0.0-dev" parses too; use IsDevVersion to skip dev builds.
func parseSemver(s string) (semver, bool) {
	var v semver
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		if i == len(s)-1 {
			return v, false
		}
		v.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return v, false
		}
		v.n[i] = x
	}
	return v, true
}

func cmpSemver(a, b semver) int {
	for i := 0; i < 3; i++ {
		if a.n[i] != b.n[i] {
			if a.n[i] > b.n[i] {
				return 1
			}
			return -1
		}
	}
	// A release is newer than any pre-release of the same version.
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, ex := strconv.Atoi(a.pre[i])
		y, ey := strconv.Atoi(b.pre[i])
		switch {
		case ex == nil && ey == nil:
			if x != y {
				if x > y {
					return 1
				}
				return -1
			}
		case ex == nil: // numeric identifiers sort before text
			return -1
		case ey == nil:
			return 1
		default:
			if c := strings.Compare(a.pre[i], b.pre[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(a.pre) > len(b.pre):
		return 1
	case len(a.pre) < len(b.pre):
		return -1
	}
	return 0
}

// NewerVersion reports whether a is newer than b (semver, optional "v" prefix).
// Unparseable versions are never newer.
func NewerVersion(a, b string) bool {
	va, ok1 := parseSemver(a)
	vb, ok2 := parseSemver(b)
	return ok1 && ok2 && cmpSemver(va, vb) > 0
}

// IsDevVersion reports whether v is an unreleased build (0.0.0-dev, empty,
// or not a version at all). Dev builds never self-update.
func IsDevVersion(v string) bool {
	s, ok := parseSemver(v)
	if !ok {
		return true
	}
	if s.n == [3]int{} {
		return true
	}
	for _, p := range s.pre {
		if p == "dev" {
			return true
		}
	}
	return false
}

// SelfUpdate downloads the new launcher named by the (signed) manifest next
// to exe, verifies it, and swaps it in. Returns true when the caller should
// restart exe.
func SelfUpdate(ctx context.Context, exe, manifestURL string, li *LauncherInfo, current string) (bool, error) {
	if li == nil || li.URL == "" || !NewerVersion(li.Version, current) {
		return false, nil
	}
	u, err := Resolve(manifestURL, li.URL)
	if err != nil {
		return false, err
	}
	if err := CanWriteDir(filepath.Dir(exe)); err != nil {
		return false, err
	}
	newPath := exe + ".new"
	os.Remove(newPath)
	if err := DownloadVerified(ctx, u, newPath, li.Size, li.SHA256, nil); err != nil {
		os.Remove(newPath)
		return false, err
	}
	if err := SwapExe(exe, newPath); err != nil {
		return false, err
	}
	return true, nil
}

// ErrNotWritable means the launcher's folder (e.g. Program Files) cannot be
// written, so it cannot replace itself.
var ErrNotWritable = errors.New("the launcher's folder is read-only")

// CanWriteDir checks that a file can be created in dir.
func CanWriteDir(dir string) error {
	f, err := os.CreateTemp(dir, ".a2launcher-write-test-*")
	if err != nil {
		return ErrNotWritable
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}

// SwapExe replaces exe with newPath. A running exe on Windows can be renamed
// but not overwritten or deleted, so exe is renamed to exe.old first (removed
// on the next start by CleanupOldExe). On failure exe is restored.
func SwapExe(exe, newPath string) error {
	return swapExe(exe, newPath, os.Rename)
}

func swapExe(exe, newPath string, rename func(a, b string) error) error {
	if _, err := os.Stat(newPath); err != nil {
		return err
	}
	old := exe + ".old"
	os.Remove(old)
	if err := rename(exe, old); err != nil {
		return err
	}
	if err := rename(newPath, exe); err != nil {
		if rerr := rename(old, exe); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return nil
}

// CleanupOldExe removes leftovers from a previous self-update (exe.old,
// exe.new). Right after a restart the old process may still be exiting and
// holding exe.old open, so it retries for a few seconds in the background.
func CleanupOldExe(exe string) {
	exe = filepath.Clean(exe)
	os.Remove(exe + ".new")
	old := exe + ".old"
	if err := os.Remove(old); err == nil || os.IsNotExist(err) {
		return
	}
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			if err := os.Remove(old); err == nil || os.IsNotExist(err) {
				return
			}
		}
	}()
}
