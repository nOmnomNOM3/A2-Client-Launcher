package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
)

// Set at build time with -ldflags "-X main.version=... -X main.manifestURL=... -X main.publicKey=...".
var (
	version     = "0.0.0-dev"
	manifestURL = "https://files.biffinboys.com/rom2/client/manifest.json"
	publicKey   = "" // base64 ed25519 public key; when set, unsigned manifests are refused
)

type ui interface {
	core.Progress
	News(text string)
}

// session holds one check/update run.
type session struct {
	cfg      core.Config
	manifest *core.Manifest
}

func (s *session) url() string {
	if s.cfg.ManifestURL != "" {
		return s.cfg.ManifestURL
	}
	return manifestURL
}

func pubKey() (ed25519.PublicKey, error) {
	if publicKey == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("this launcher was built with a broken signing key")
	}
	return ed25519.PublicKey(b), nil
}

// update fetches the manifest, self-updates if needed (restart=true), and
// brings the game folder up to date. full=true re-hashes every file (Repair).
func (s *session) update(ctx context.Context, u ui, full bool) (restart bool, err error) {
	u.Status("Checking for updates…")
	data, err := core.FetchSmall(ctx, s.url(), 8<<20)
	if err != nil {
		return false, err
	}
	pub, err := pubKey()
	if err != nil {
		return false, err
	}
	var sig []byte
	if pub != nil {
		if sig, err = core.FetchSmall(ctx, s.url()+".sig", 4096); err != nil {
			return false, err
		}
	}
	m, err := core.ParseManifest(data, sig, pub)
	if err != nil {
		return false, err
	}
	s.manifest = m
	u.News(m.News)

	if exe, err := os.Executable(); err == nil && m.Launcher != nil && core.NewerVersion(m.Launcher.Version, version) {
		u.Status("Updating the launcher…")
		ok, err := core.SelfUpdate(ctx, exe, s.url(), m.Launcher, version)
		if err != nil {
			return false, fmt.Errorf("launcher self-update failed: %v", err)
		}
		if ok {
			return true, nil
		}
	}

	root := s.cfg.InstallDir
	if err := os.MkdirAll(root, 0o755); err != nil {
		return false, fmt.Errorf("cannot create the game folder %s: %v", root, err)
	}
	cache := core.LoadHashCache(root)
	plan, err := core.Diff(root, m, cache, full, func(done, total int, name string) {
		u.Status(fmt.Sprintf("Checking files %d/%d", done, total))
		u.Bytes(int64(done), int64(total))
	})
	if err != nil {
		return false, fmt.Errorf("could not read the game folder: %v", err)
	}
	cache.Save()
	if len(plan.Fetch) == 0 && len(plan.Delete) == 0 {
		u.Bytes(1, 1)
		u.Status("Up to date (version " + m.Version + ")")
		return false, nil
	}
	if err := core.Apply(ctx, root, s.url(), m, plan, cache, u); err != nil {
		return false, err
	}
	u.Bytes(1, 1)
	u.Status(fmt.Sprintf("Updated %d file(s) — version %s", len(plan.Fetch), m.Version))
	return false, nil
}

// startGame launches the game process without waiting.
func (s *session) startGame() (*os.Process, error) {
	exe, args := "allods2.exe", []string{"-hat", "biffinboys.ddns.net"}
	if s.manifest != nil {
		exe, args = s.manifest.Exe, s.manifest.Args
	}
	p := filepath.Join(s.cfg.InstallDir, filepath.FromSlash(exe))
	if _, err := os.Stat(p); err != nil {
		return nil, fmt.Errorf("the game is not installed yet (%s missing). Click Play while online to download it", exe)
	}
	cmd := exec.Command(p, args...)
	cmd.Dir = s.cfg.InstallDir
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start the game: %v", err)
	}
	return cmd.Process, nil
}

// watchGame waits for the process, then returns a crash-report zip path if the
// run looked like a crash. A clean exit returns ("", nil).
func (s *session) watchGame(proc *os.Process, before map[string]core.LogStat) (string, error) {
	state, err := proc.Wait()
	code := 0
	if state != nil {
		code = state.ExitCode()
	}
	if err != nil && state == nil {
		return "", fmt.Errorf("game process: %v", err)
	}
	ver := ""
	if s.manifest != nil {
		ver = s.manifest.Version
	}
	det := core.DetectCrash(s.cfg.InstallDir, code, before)
	if !det.Crashed {
		return "", nil
	}
	return core.WriteCrashReport(s.cfg.InstallDir, det, ver)
}

func restartSelf() {
	exe, _ := os.Executable()
	exec.Command(exe, os.Args[1:]...).Start()
	os.Exit(0)
}

func installDir(cfg core.Config) string {
	if cfg.InstallDir != "" {
		return cfg.InstallDir
	}
	// Running from inside an existing game folder: use it.
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(d, "allods2.exe")); err == nil && !strings.Contains(strings.ToLower(d), "program files") {
			return d
		}
	}
	return core.DefaultInstallDir()
}
