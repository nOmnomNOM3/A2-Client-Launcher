package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestClearCharCache(t *testing.T) {
	root := t.TempDir()
	keep := []string{"famehall.dat", "hotkey.3203272371.a2key", "allods2.exe", "patch.res", "notes.a2c.txt", "maps/x.a2c", "logs/y.a2c", "chr/z.a2c"}
	del := []string{"311777688826776.a2c", "39952057926569.a2c", "UPPER.A2C"}
	for _, f := range append(append([]string{}, keep...), del...) {
		p := filepath.Join(root, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.Mkdir(filepath.Join(root, "dir.a2c"), 0o755) // a folder named like a cache file is left alone

	if n := ClearCharCache(root); n != len(del) {
		t.Fatalf("deleted %d, want %d", n, len(del))
	}
	for _, f := range del {
		if _, err := os.Stat(filepath.Join(root, f)); !os.IsNotExist(err) {
			t.Errorf("%s still there", f)
		}
	}
	for _, f := range append(keep, "dir.a2c") {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s was removed: %v", f, err)
		}
	}
	log, _ := os.ReadFile(LogPath(root))
	var got []string
	for _, l := range strings.Split(string(log), "\n") {
		if i := strings.Index(l, "a2c cache: deleted "); i >= 0 {
			got = append(got, l[i+len("a2c cache: deleted "):])
		}
	}
	sort.Strings(got)
	want := append([]string{}, del...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("log lists %v, want %v", got, want)
	}
	if n := ClearCharCache(root); n != 0 {
		t.Fatalf("second run deleted %d", n)
	}
	if n := ClearCharCache(filepath.Join(root, "missing")); n != 0 {
		t.Fatalf("missing folder deleted %d", n)
	}
}

func TestIsCharCacheFile(t *testing.T) {
	for rel, want := range map[string]bool{
		"1.a2c": true, "X.A2C": true, "a/1.a2c": false, `a\1.a2c`: false, "1.a2key": false, "famehall.dat": false, "1.a2c.bak": false,
	} {
		if got := IsCharCacheFile(rel); got != want {
			t.Errorf("%q = %v, want %v", rel, got, want)
		}
	}
}

func TestDiffIgnoresCharCache(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "1.a2c"), []byte("stale"), 0o644)
	m := &Manifest{Files: []ManifestFile{{Path: "1.a2c", Size: 3, SHA256: strings.Repeat("0", 64)}}, Delete: []string{"1.a2c"}}
	p, err := Diff(root, m, LoadHashCache(root), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Fetch) != 0 || len(p.Delete) != 0 {
		t.Fatalf("plan touches .a2c: %+v", p)
	}
}
