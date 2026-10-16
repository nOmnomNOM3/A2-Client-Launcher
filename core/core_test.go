package core

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type nopProgress struct{}

func (nopProgress) Status(string)      {}
func (nopProgress) Bytes(int64, int64) {}

func write(t *testing.T, root, rel, body string) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mf(rel, body string) ManifestFile {
	return ManifestFile{Path: rel, Size: int64(len(body)), SHA256: sha256Hex([]byte(body))}
}

// server serves objects by hash; supports Range; can corrupt the first reply.
func server(t *testing.T, objs map[string]string, corruptOnce *int32, rangeHits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sha := strings.TrimPrefix(r.URL.Path, "/objects/")
		body, ok := objs[sha]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if corruptOnce != nil && atomic.CompareAndSwapInt32(corruptOnce, 1, 0) {
			w.Write([]byte(strings.Repeat("X", len(body))))
			return
		}
		if r.Header.Get("Range") != "" && rangeHits != nil {
			atomic.AddInt32(rangeHits, 1)
		}
		http.ServeContent(w, r, "", time0, strings.NewReader(body))
	}))
}

func TestManifestValidation(t *testing.T) {
	good := `{"format":1,"exe":"allods2.exe","files":[{"path":"data/world.res","size":1,"sha256":"` + strings.Repeat("a", 64) + `"}]}`
	if _, err := ParseManifest([]byte(good), nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x", "/etc/x", `C:/x`, `a\b`, "a/../../b", ".a2launcher/x", "./a"} {
		m := strings.Replace(good, "data/world.res", strings.ReplaceAll(bad, `\`, `\\`), 1)
		if _, err := ParseManifest([]byte(m), nil, nil); err == nil {
			t.Errorf("accepted unsafe path %q", bad)
		}
	}
	dup := strings.Replace(good, `}]}`, `},{"path":"DATA/world.res","size":1,"sha256":"`+strings.Repeat("a", 64)+`"}]}`, 1)
	if _, err := ParseManifest([]byte(dup), nil, nil); err == nil {
		t.Error("accepted duplicate path")
	}
}

func TestSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	data := []byte(`{"format":1,"exe":"allods2.exe","files":[]}`)
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)))
	if _, err := ParseManifest(data, sig, pub); err != nil {
		t.Fatal(err)
	}
	tampered := []byte(`{"format":1,"exe":"evil.exe","files":[]}`)
	if _, err := ParseManifest(tampered, sig, pub); err == nil {
		t.Fatal("tampered manifest accepted")
	}
	if _, err := ParseManifest(data, nil, pub); err == nil {
		t.Fatal("missing signature accepted")
	}
}

func TestDiff(t *testing.T) {
	root := t.TempDir()
	write(t, root, "same.res", "same")
	write(t, root, "changed.res", "old")
	write(t, root, "allods2.cfg", "player settings")
	write(t, root, "obsolete.dll", "x")
	write(t, root, "extra.txt", "not ours")
	m := &Manifest{Format: 1, Exe: "allods2.exe", Delete: []string{"obsolete.dll", "gone.dll"}, Files: []ManifestFile{
		mf("same.res", "same"), mf("changed.res", "new"), mf("maps/new.alm", "map"),
		func() ManifestFile { f := mf("allods2.cfg", "default"); f.Once = true; return f }(),
	}}
	p, err := Diff(root, m, LoadHashCache(root), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range p.Fetch {
		got = append(got, f.Path)
	}
	if strings.Join(got, ",") != "changed.res,maps/new.alm" {
		t.Fatalf("fetch = %v", got)
	}
	if strings.Join(p.Delete, ",") != "obsolete.dll" || p.TotalBytes != 6 {
		t.Fatalf("delete=%v total=%d", p.Delete, p.TotalBytes)
	}
}

func TestHashCacheDetectsEdits(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.res", "one")
	c := LoadHashCache(root)
	h1, _, _ := c.LocalHash(root, "a.res", false)
	c.Save()
	write(t, root, "a.res", "two!") // size differs
	c2 := LoadHashCache(root)
	h2, _, _ := c2.LocalHash(root, "a.res", false)
	if h1 == h2 || h2 != sha256Hex([]byte("two!")) {
		t.Fatal("cache returned stale hash")
	}
}

func TestApplyEndToEnd(t *testing.T) {
	root := t.TempDir()
	write(t, root, "world.res", "old world")
	write(t, root, "obsolete.dll", "bye")
	files := map[string]string{"world.res": "new world data", "maps/a.alm": "map a", "maps/copy.alm": "map a"}
	objs := map[string]string{}
	m := &Manifest{Format: 1, Exe: "allods2.exe", Delete: []string{"obsolete.dll"}}
	for _, k := range []string{"world.res", "maps/a.alm", "maps/copy.alm"} {
		f := mf(k, files[k])
		m.Files = append(m.Files, f)
		objs[f.SHA256] = files[k]
	}
	corrupt := int32(1)
	srv := server(t, objs, &corrupt, nil)
	defer srv.Close()
	murl := srv.URL + "/manifest.json"
	cache := LoadHashCache(root)
	p, _ := Diff(root, m, cache, true, nil)
	if err := Apply(context.Background(), root, murl, m, p, cache, nopProgress{}); err != nil {
		t.Fatal(err)
	}
	for k, v := range files {
		b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(k)))
		if string(b) != v {
			t.Fatalf("%s = %q", k, b)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "obsolete.dll")); !os.IsNotExist(err) {
		t.Fatal("obsolete file not removed")
	}
	bk, _ := filepath.Glob(filepath.Join(root, StateDir, "backup", "*", "world.res"))
	if len(bk) != 1 {
		t.Fatal("old world.res not backed up")
	}
	p2, _ := Diff(root, m, LoadHashCache(root), true, nil)
	if len(p2.Fetch) != 0 {
		t.Fatalf("still out of date after apply: %v", p2.Fetch)
	}
}

func TestResume(t *testing.T) {
	body := strings.Repeat("0123456789", 10000)
	sha := sha256Hex([]byte(body))
	var ranges int32
	srv := server(t, map[string]string{sha: body}, nil, &ranges)
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "f.part")
	os.WriteFile(dst, []byte(body[:40000]), 0o644)
	var n int64
	if err := DownloadVerified(context.Background(), srv.URL+"/objects/"+sha, dst, int64(len(body)), sha, func(b int64) { n += b }); err != nil {
		t.Fatal(err)
	}
	if ranges != 1 || n != int64(len(body)) {
		t.Fatalf("ranges=%d bytes=%d", ranges, n)
	}
}

func TestBadDownloadFails(t *testing.T) {
	sha := strings.Repeat("b", 64)
	srv := server(t, map[string]string{sha: "not matching"}, nil, nil)
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "f.part")
	if err := DownloadVerified(context.Background(), srv.URL+"/objects/"+sha, dst, 12, sha, nil); err == nil {
		t.Fatal("bad checksum accepted")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("corrupt file left behind")
	}
}

func TestSelfUpdate(t *testing.T) {
	newExe := "new launcher bytes"
	sha := sha256Hex([]byte(newExe))
	srv := server(t, map[string]string{sha: newExe}, nil, nil)
	defer srv.Close()
	exe := filepath.Join(t.TempDir(), "launcher.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	li := &LauncherInfo{Version: "1.0.10", URL: "objects/" + sha, SHA256: sha, Size: int64(len(newExe))}
	ok, err := SelfUpdate(context.Background(), exe, srv.URL+"/manifest.json", li, "1.0.9")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	b, _ := os.ReadFile(exe)
	if string(b) != newExe {
		t.Fatal("exe not replaced")
	}
	if ok, _ := SelfUpdate(context.Background(), exe, srv.URL+"/manifest.json", li, "1.0.10"); ok {
		t.Fatal("updated to same version")
	}
}

func TestManifestJSONRoundTrip(t *testing.T) {
	m := Manifest{Format: 1, Exe: "allods2.exe", Args: []string{"-hat", "biffinboys.ddns.net"}}
	b, _ := json.Marshal(m)
	if _, err := ParseManifest(b, nil, nil); err != nil {
		t.Fatal(err)
	}
}

var time0 = time.Unix(0, 0)

func TestGameInstalled(t *testing.T) {
	d := t.TempDir()
	if GameInstalled(d) || GameInstalled("") {
		t.Fatal("empty dir counted as installed")
	}
	os.WriteFile(filepath.Join(d, "allods2.exe"), []byte("x"), 0o644)
	if !GameInstalled(d) {
		t.Fatal("allods2.exe not detected")
	}
}
