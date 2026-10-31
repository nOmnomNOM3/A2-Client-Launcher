package core

import (
	"os"
	"path/filepath"
	"strings"
)

// allods2.exe caches each character it gets from the Hat as "<id1><id2>.a2c"
// in its working folder (the game folder, not a subfolder). On login it
// requests every character the Hat lists and recreates a missing file from the
// Hat's data, but it can keep an existing stale copy, and a stale copy with a
// corrupt item crashes character select. Deleting them before each launch
// makes the client always rebuild them from the Hat.

// IsCharCacheFile reports whether a game-folder-relative path is a client
// character cache file (top-level *.a2c).
func IsCharCacheFile(rel string) bool {
	return !strings.ContainsAny(rel, `/\`) && strings.EqualFold(filepath.Ext(rel), ".a2c")
}

// ClearCharCache deletes the top-level *.a2c files in the game folder (not
// recursive, nothing else). Each deletion and failure is logged; errors never
// stop the caller. It returns how many files were deleted.
func ClearCharCache(root string) int {
	ents, err := os.ReadDir(root)
	if err != nil {
		Logf(root, "a2c cache: cannot list %s: %v", root, err)
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.Type().IsRegular() || !IsCharCacheFile(e.Name()) {
			continue
		}
		p := filepath.Join(root, e.Name())
		if err := os.Remove(p); err != nil {
			Logf(root, "a2c cache: could not delete %s: %v", e.Name(), err)
			continue
		}
		Logf(root, "a2c cache: deleted %s", e.Name())
		n++
	}
	return n
}
