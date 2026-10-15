package core

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) string {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for n, body := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return b.String()
}

func packSetup(t *testing.T, n int) (*Manifest, map[string]string, []ManifestFile) {
	objs := map[string]string{}
	content := map[string]string{}
	var files []ManifestFile
	var paths []string
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("data/f%03d.bin", i)
		body := fmt.Sprintf("body-%d", i)
		f := mf(p, body)
		files = append(files, f)
		objs[f.SHA256] = body
		content[p] = body
		paths = append(paths, p)
	}
	z := makeZip(t, content)
	sha := sha256Hex([]byte(z))
	objs[sha] = z
	m := &Manifest{Format: 1, Exe: "allods2.exe", Files: files,
		Packs: []Pack{{SHA256: sha, Size: int64(len(z)), Files: paths}}}
	return m, objs, files
}

func checkTmp(t *testing.T, tmp string, files []ManifestFile, objs map[string]string) {
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(tmp, f.SHA256+".part"))
		if err != nil || string(b) != objs[f.SHA256] {
			t.Fatalf("%s missing or wrong: %v", f.Path, err)
		}
	}
}

func TestPackGood(t *testing.T) {
	m, objs, files := packSetup(t, 10)
	var reqs int32
	srv := server(t, objs, nil, nil)
	defer srv.Close()
	inner := srv.Config.Handler
	srv.Config.Handler = countHandler(inner, &reqs)
	RetryBase = 0
	tmp := t.TempDir()
	if err := fetchAll(context.Background(), tmp, srv.URL+"/manifest.json", m, files, nopProgress{}); err != nil {
		t.Fatal(err)
	}
	checkTmp(t, tmp, files, objs)
	if reqs != 1 {
		t.Fatalf("want 1 request (the pack), got %d", reqs)
	}
}

func TestPackBelowThresholdUsesFiles(t *testing.T) {
	m, objs, files := packSetup(t, 10)
	var reqs int32
	srv := server(t, objs, nil, nil)
	defer srv.Close()
	srv.Config.Handler = countHandler(srv.Config.Handler, &reqs)
	tmp := t.TempDir()
	if err := fetchAll(context.Background(), tmp, srv.URL+"/manifest.json", m, files[:2], nopProgress{}); err != nil {
		t.Fatal(err)
	}
	checkTmp(t, tmp, files[:2], objs)
	if reqs != 2 {
		t.Fatalf("want 2 per-file requests, got %d", reqs)
	}
}

func TestPackCorruptFallsBack(t *testing.T) {
	m, objs, files := packSetup(t, 10)
	RetryBase = 0
	objs[m.Packs[0].SHA256] = "garbage!" // wrong hash every time
	m.Packs[0].Size = int64(len("garbage!"))
	srv := server(t, objs, nil, nil)
	defer srv.Close()
	tmp := t.TempDir()
	if err := fetchAll(context.Background(), tmp, srv.URL+"/manifest.json", m, files, nopProgress{}); err != nil {
		t.Fatal(err)
	}
	checkTmp(t, tmp, files, objs)
}

func TestPackBadFileContentFallsBack(t *testing.T) {
	m, objs, files := packSetup(t, 4)
	// pack hash is valid but one file inside does not match the manifest
	content := map[string]string{}
	for _, f := range files {
		content[f.Path] = objs[f.SHA256]
	}
	content[files[2].Path] = "body-X"
	z := makeZip(t, content)
	sha := sha256Hex([]byte(z))
	objs[sha] = z
	m.Packs[0].SHA256, m.Packs[0].Size = sha, int64(len(z))
	srv := server(t, objs, nil, nil)
	defer srv.Close()
	tmp := t.TempDir()
	if err := fetchAll(context.Background(), tmp, srv.URL+"/manifest.json", m, files, nopProgress{}); err != nil {
		t.Fatal(err)
	}
	checkTmp(t, tmp, files, objs)
}

func TestPackPathTraversal(t *testing.T) {
	m, objs, files := packSetup(t, 3)
	content := map[string]string{"../evil.txt": "pwned"}
	for _, f := range files {
		content[f.Path] = objs[f.SHA256]
	}
	z := makeZip(t, content)
	sha := sha256Hex([]byte(z))
	objs[sha] = z
	m.Packs[0].SHA256, m.Packs[0].Size = sha, int64(len(z))
	srv := server(t, objs, nil, nil)
	defer srv.Close()
	root := t.TempDir()
	tmp := filepath.Join(root, "a", "b")
	os.MkdirAll(tmp, 0o755)
	want := map[string]ManifestFile{}
	for _, f := range files {
		want[f.Path] = f
	}
	zp := filepath.Join(root, "p.zip")
	os.WriteFile(zp, []byte(z), 0o644)
	if _, err := extractPack(zp, tmp, want); err == nil {
		t.Fatal("unsafe pack entry accepted")
	}
	// full fetch still succeeds via per-file fallback, and nothing escapes
	if err := fetchAll(context.Background(), tmp, srv.URL+"/manifest.json", m, files, nopProgress{}); err != nil {
		t.Fatal(err)
	}
	checkTmp(t, tmp, files, objs)
	for _, p := range []string{filepath.Join(root, "a", "evil.txt"), filepath.Join(root, "evil.txt")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatal("file escaped")
		}
	}
	// manifests with unsafe pack paths are rejected outright
	bad := `{"format":1,"exe":"allods2.exe","files":[],"packs":[{"sha256":"` + sha + `","size":1,"files":["../x"]}]}`
	if _, err := ParseManifest([]byte(bad), nil, nil); err == nil {
		t.Fatal("manifest with unsafe pack path accepted")
	}
}

func TestParallelManyFiles(t *testing.T) {
	m, objs, files := packSetup(t, 60)
	m.Packs = nil // old-style manifest
	srv := server(t, objs, nil, nil)
	defer srv.Close()
	tmp := t.TempDir()
	if err := fetchAll(context.Background(), tmp, srv.URL+"/manifest.json", m, files, nopProgress{}); err != nil {
		t.Fatal(err)
	}
	checkTmp(t, tmp, files, objs)
}

var _ = atomic.AddInt32

func countHandler(h http.Handler, n *int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(n, 1); h.ServeHTTP(w, r) })
}
