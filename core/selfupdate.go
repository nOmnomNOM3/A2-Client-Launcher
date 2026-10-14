package core

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// NewerVersion reports whether a is newer than b (dotted numbers, e.g. 1.2.10).
func NewerVersion(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// SelfUpdate downloads the new launcher next to exe, verifies it, and swaps
// it in (a running exe on Windows can be renamed but not overwritten).
// Returns true when the caller should restart exe.
func SelfUpdate(ctx context.Context, exe, manifestURL string, li *LauncherInfo, current string) (bool, error) {
	if li == nil || li.URL == "" || !NewerVersion(li.Version, current) {
		return false, nil
	}
	u, err := Resolve(manifestURL, li.URL)
	if err != nil {
		return false, err
	}
	newPath := exe + ".new"
	os.Remove(newPath)
	if err := DownloadVerified(ctx, u, newPath, li.Size, li.SHA256, nil); err != nil {
		return false, err
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return false, err
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(old, exe)
		return false, err
	}
	return true, nil
}

// CleanupOldExe removes the leftover from a previous self-update.
func CleanupOldExe(exe string) { os.Remove(filepath.Clean(exe) + ".old") }
