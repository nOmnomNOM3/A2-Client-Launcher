package core

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.3.1", "1.3.0", true},
		{"v1.3.1", "1.3.0", true},
		{"1.3.0", "v1.3.0", false},
		{"1.2.10", "1.2.9", true},
		{"1.10.0", "1.9.9", true},
		{"2.0.0", "1.99.99", true},
		{"1.3.0", "1.3.1", false},
		{"1.4", "1.3.9", true},
		{"1.3.1", "1.3.1-rc.1", true},
		{"1.3.1-rc.1", "1.3.1", false},
		{"1.3.1-rc.2", "1.3.1-rc.1", true},
		{"1.3.1-rc.10", "1.3.1-rc.9", true},
		{"1.3.1-beta", "1.3.1-alpha", true},
		{"1.3.1+build5", "1.3.1", false},
		{"1.3.0", "0.0.0-dev", true},
		{"garbage", "1.0.0", false},
		{"1.0.0", "garbage", false},
		{"", "1.0.0", false},
	}
	for _, c := range cases {
		if got := NewerVersion(c.a, c.b); got != c.want {
			t.Errorf("NewerVersion(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestIsDevVersion(t *testing.T) {
	for v, want := range map[string]bool{"0.0.0-dev": true, "": true, "dev": true, "1.3.0-dev": true, "1.3.0": false, "v1.3.1": false, "1.3.1-rc.1": false} {
		if IsDevVersion(v) != want {
			t.Errorf("IsDevVersion(%q) want %v", v, want)
		}
	}
}

func TestSwapExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "RoM2 Launcher.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(exe+".new", []byte("new"), 0o755)
	if err := SwapExe(exe, exe+".new"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("exe = %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Fatalf("old = %q", b)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal(".new left behind")
	}
	CleanupOldExe(exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old not cleaned up")
	}
}

func TestSwapExeMissingNewKeepsOld(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "launcher.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := SwapExe(exe, exe+".new"); err == nil {
		t.Fatal("swap without new file succeeded")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("exe = %q", b)
	}
}

func TestSwapExeRollbackWhenMoveFails(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "launcher.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(exe+".new", []byte("new"), 0o755)
	// Moving the new exe into place fails (e.g. antivirus holds it).
	rename := func(a, b string) error {
		if a == exe+".new" {
			return os.ErrPermission
		}
		return os.Rename(a, b)
	}
	if err := swapExe(exe, exe+".new", rename); err == nil {
		t.Fatal("swap reported success")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("exe not restored: %q", b)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old left behind after rollback")
	}
}

func TestCleanupOldExeRemovesNew(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "launcher.exe")
	os.WriteFile(exe+".old", []byte("x"), 0o644)
	os.WriteFile(exe+".new", []byte("x"), 0o644)
	CleanupOldExe(exe)
	for _, p := range []string{exe + ".old", exe + ".new"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", p)
		}
	}
}

// fakeGitHub serves a release API response and its assets.
type fakeGitHub struct {
	tag     string
	exe     []byte
	digest  bool
	sig     []byte // nil: no .sig asset
	sha     bool   // .sha256 asset
	failAPI bool
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			if f.failAPI {
				http.Error(w, "rate limited", 403)
				return
			}
			rel := GHRelease{TagName: f.tag}
			a := GHAsset{Name: LauncherAsset, Size: int64(len(f.exe)), URL: srv.URL + "/dl/exe"}
			if f.digest {
				a.Digest = "sha256:" + sha256Hex(f.exe)
			}
			rel.Assets = append(rel.Assets, a)
			if f.sig != nil {
				rel.Assets = append(rel.Assets, GHAsset{Name: LauncherAsset + ".sig", Size: 1, URL: srv.URL + "/dl/sig"})
			}
			if f.sha {
				rel.Assets = append(rel.Assets, GHAsset{Name: LauncherAsset + ".sha256", Size: 1, URL: srv.URL + "/dl/sha"})
			}
			json.NewEncoder(w).Encode(rel)
		case "/dl/exe":
			w.Write(f.exe)
		case "/dl/sig":
			w.Write([]byte(base64.StdEncoding.EncodeToString(f.sig) + "\n"))
		case "/dl/sha":
			w.Write([]byte(sha256Hex(f.exe) + "  RoM2 Launcher.exe\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ghExe(t *testing.T) string {
	exe := filepath.Join(t.TempDir(), "RoM2 Launcher.exe")
	os.WriteFile(exe, []byte("old launcher"), 0o755)
	return exe
}

func runGH(t *testing.T, f *fakeGitHub, exe string, o GHUpdate) (string, error) {
	srv := f.server(t)
	o.URL = srv.URL + "/latest"
	if o.Current == "" {
		o.Current = "1.3.0"
	}
	o.Log = t.Logf
	return GitHubSelfUpdate(context.Background(), exe, o)
}

func wantExe(t *testing.T, exe, content string) {
	t.Helper()
	if b, _ := os.ReadFile(exe); string(b) != content {
		t.Fatalf("exe = %q want %q", b, content)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal(".new left behind")
	}
}

func TestGitHubSelfUpdateDigest(t *testing.T) {
	exe := ghExe(t)
	var status []string
	tag, err := runGH(t, &fakeGitHub{tag: "v1.3.1", exe: []byte("new launcher"), digest: true}, exe,
		GHUpdate{Status: func(s string) { status = append(status, s) }})
	if err != nil || tag != "v1.3.1" {
		t.Fatal(tag, err)
	}
	wantExe(t, exe, "new launcher")
	if len(status) != 1 || status[0] != "Updating launcher…" {
		t.Fatalf("status %q", status)
	}
}

func TestGitHubSelfUpdateSha256Asset(t *testing.T) {
	exe := ghExe(t)
	tag, err := runGH(t, &fakeGitHub{tag: "v1.3.1", exe: []byte("new launcher"), sha: true}, exe, GHUpdate{})
	if err != nil || tag == "" {
		t.Fatal(tag, err)
	}
	wantExe(t, exe, "new launcher")
}

func TestGitHubSelfUpdateNoHashSkips(t *testing.T) {
	exe := ghExe(t)
	tag, err := runGH(t, &fakeGitHub{tag: "v1.3.1", exe: []byte("new launcher")}, exe, GHUpdate{})
	if err != nil || tag != "" {
		t.Fatal(tag, err)
	}
	wantExe(t, exe, "old launcher")
}

func TestGitHubSelfUpdateBadDigestRefused(t *testing.T) {
	exe := ghExe(t)
	f := &fakeGitHub{tag: "v1.3.1", exe: []byte("new launcher"), digest: true}
	srv := f.server(t)
	// Serve a digest for different bytes.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(GHRelease{TagName: "v1.3.1", Assets: []GHAsset{{Name: LauncherAsset, Size: 12, Digest: "sha256:" + sha256Hex([]byte("evil bytes!!")), URL: srv.URL + "/dl/exe"}}})
	}))
	defer bad.Close()
	tag, err := GitHubSelfUpdate(context.Background(), exe, GHUpdate{URL: bad.URL, Current: "1.3.0", Log: t.Logf})
	if err == nil || tag != "" {
		t.Fatal("bad digest accepted", tag)
	}
	wantExe(t, exe, "old launcher")
}

func TestGitHubSelfUpdateSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	newExe := []byte("signed launcher")
	good := ed25519.Sign(priv, LauncherSigMessage("1.3.1", sha256Hex(newExe)))

	exe := ghExe(t)
	tag, err := runGH(t, &fakeGitHub{tag: "v1.3.1", exe: newExe, sig: good}, exe, GHUpdate{PubKey: pub, RequireSig: true})
	if err != nil || tag != "v1.3.1" {
		t.Fatal(tag, err)
	}
	wantExe(t, exe, "signed launcher")

	// Signature for another version (replay of an old signed exe) is refused.
	exe = ghExe(t)
	old := ed25519.Sign(priv, LauncherSigMessage("1.3.0", sha256Hex(newExe)))
	if tag, err := runGH(t, &fakeGitHub{tag: "v1.3.1", exe: newExe, sig: old, digest: true}, exe, GHUpdate{PubKey: pub}); err == nil || tag != "" {
		t.Fatal("wrong-version signature accepted")
	}
	wantExe(t, exe, "old launcher")

	// Signature by another key is refused even though the digest matches.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	exe = ghExe(t)
	forged := ed25519.Sign(other, LauncherSigMessage("1.3.1", sha256Hex(newExe)))
	if tag, err := runGH(t, &fakeGitHub{tag: "v1.3.1", exe: newExe, sig: forged, digest: true}, exe, GHUpdate{PubKey: pub}); err == nil || tag != "" {
		t.Fatal("forged signature accepted")
	}
	wantExe(t, exe, "old launcher")
}

func TestGitHubSelfUpdateRequireSigWithoutSig(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	exe := ghExe(t)
	tag, err := runGH(t, &fakeGitHub{tag: "v1.3.1", exe: []byte("new launcher"), digest: true}, exe, GHUpdate{PubKey: pub, RequireSig: true})
	if err != nil || tag != "" {
		t.Fatal(tag, err)
	}
	wantExe(t, exe, "old launcher")
}

func TestGitHubSelfUpdateSkips(t *testing.T) {
	f := func() *fakeGitHub { return &fakeGitHub{tag: "v1.3.1", exe: []byte("new launcher"), digest: true} }
	for name, o := range map[string]GHUpdate{
		"same version": {Current: "1.3.1"},
		"newer local":  {Current: "1.4.0"},
		"dev build":    {Current: "0.0.0-dev"},
		"loop guard":   {SkipTag: "v1.3.1"},
	} {
		exe := ghExe(t)
		if tag, err := runGH(t, f(), exe, o); err != nil || tag != "" {
			t.Errorf("%s: tag=%q err=%v", name, tag, err)
		}
		wantExe(t, exe, "old launcher")
	}
}

func TestGitHubSelfUpdateAPIFailure(t *testing.T) {
	exe := ghExe(t)
	tag, err := runGH(t, &fakeGitHub{failAPI: true}, exe, GHUpdate{})
	if err == nil || tag != "" || !strings.Contains(err.Error(), "403") {
		t.Fatal(tag, err)
	}
	wantExe(t, exe, "old launcher")

	// Unreachable/slow API: gives up after the timeout.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	start := time.Now()
	tag, err = GitHubSelfUpdate(context.Background(), exe, GHUpdate{URL: slow.URL, Current: "1.3.0", Timeout: 200 * time.Millisecond})
	if err == nil || tag != "" || time.Since(start) > 3*time.Second {
		t.Fatal("slow API not timed out", tag, err, time.Since(start))
	}
}

func TestGitHubSelfUpdateReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root POSIX user for a read-only folder")
	}
	exe := ghExe(t)
	dir := filepath.Dir(exe)
	os.Chmod(dir, 0o555)
	defer os.Chmod(dir, 0o755)
	tag, err := runGH(t, &fakeGitHub{tag: "v1.3.1", exe: []byte("new launcher"), digest: true}, exe, GHUpdate{})
	if err != nil || tag != "" {
		t.Fatal(tag, err)
	}
	wantExe(t, exe, "old launcher")
}
