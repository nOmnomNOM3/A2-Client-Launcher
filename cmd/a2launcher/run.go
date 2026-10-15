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
	"time"

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
	core.Logf(root, "update: launcher %s, manifest %s", version, m.Version)
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

// earlyExitWindow: a game that dies this fast with a non-zero code failed to start.
var earlyExitWindow = 5 * time.Second

// gameRun is a started game process. done receives exactly one exit result.
type gameRun struct {
	proc    *os.Process
	cmdline string
	done    chan exitResult
}

type exitResult struct {
	code int
	err  error
}

// gamePaths resolves the absolute game folder and exe path from the manifest.
func (s *session) gamePaths() (dir, exe string, args []string, err error) {
	rel, args := "allods2.exe", []string{"-hat", "biffinboys.ddns.net"}
	if s.manifest != nil && s.manifest.Exe != "" {
		rel, args = s.manifest.Exe, s.manifest.Args
	}
	dir, err = filepath.Abs(s.cfg.InstallDir)
	if err != nil {
		return "", "", nil, fmt.Errorf("bad game folder %q: %v", s.cfg.InstallDir, err)
	}
	exe = filepath.Join(dir, filepath.FromSlash(rel))
	return dir, exe, args, nil
}

func quoteArg(a string) string {
	if a == "" || strings.ContainsAny(a, " \t\"") {
		return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	return a
}

// startGame launches the game with cwd = game folder and starts a waiter.
func (s *session) startGame() (*gameRun, error) {
	dir, exe, args, err := s.gamePaths()
	if err != nil {
		core.Logf(s.cfg.InstallDir, "launch: %v", err)
		return nil, err
	}
	parts := []string{quoteArg(exe)}
	for _, a := range args {
		parts = append(parts, quoteArg(a))
	}
	cmdline := strings.Join(parts, " ")
	core.Logf(dir, "launch: dir=%s", dir)
	core.Logf(dir, "launch: command=%s", cmdline)
	if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
		core.Logf(dir, "launch: exe missing: %v", err)
		return nil, fmt.Errorf("the game is not installed yet (%s missing). Click Play while online to download it", exe)
	}
	for _, dll := range []string{"a2mgr.dll", "ddraw.dll"} {
		if _, err := os.Stat(filepath.Join(dir, dll)); err != nil {
			core.Logf(dir, "launch: warning: %s not found in game folder", dll)
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		core.Logf(dir, "launch: start failed: %v", err)
		return nil, fmt.Errorf("could not start the game:\n%s\n\n%v\n\nTry Repair files. If it says elevation is required, run the game folder outside Program Files. Details: %s", cmdline, err, core.LogPath(dir))
	}
	core.Logf(dir, "launch: started pid %d", cmd.Process.Pid)
	r := &gameRun{proc: cmd.Process, cmdline: cmdline, done: make(chan exitResult, 1)}
	go func() {
		st, err := cmd.Process.Wait()
		res := exitResult{err: err}
		if st != nil {
			res.code = st.ExitCode()
			res.err = nil
		}
		core.Logf(dir, "game exited: code=%d err=%v", res.code, res.err)
		r.done <- res
	}()
	return r, nil
}

// earlyCheck waits up to earlyExitWindow. It returns (exited, result, error):
// a non-zero exit inside the window becomes a user-facing error.
func (s *session) earlyCheck(r *gameRun) (bool, exitResult, error) {
	select {
	case res := <-r.done:
		if res.err != nil || res.code != 0 {
			core.Logf(s.cfg.InstallDir, "launch: game quit right away (code %d)", res.code)
			return true, res, fmt.Errorf("the game closed right after starting (exit code %d, 0x%X).\n\nCommand: %s\n\nUsually a missing or blocked file (a2mgr.dll, ddraw.dll, world.res) or antivirus. Try Repair files. Log: %s",
				res.code, uint32(res.code), r.cmdline, core.LogPath(s.cfg.InstallDir))
		}
		return true, res, nil
	case <-time.After(earlyExitWindow):
		return false, exitResult{}, nil
	}
}

// watchGame waits for the process (unless it already exited), then returns a
// crash-report zip path if the run looked like a crash. A clean exit returns ("", nil).
func (s *session) watchGame(r *gameRun, exited bool, res exitResult, before map[string]core.LogStat) (string, error) {
	if !exited {
		res = <-r.done
	}
	if res.err != nil {
		return "", fmt.Errorf("game process: %v", res.err)
	}
	ver := ""
	if s.manifest != nil {
		ver = s.manifest.Version
	}
	det := core.DetectCrash(s.cfg.InstallDir, res.code, before)
	if !det.Crashed {
		return "", nil
	}
	core.Logf(s.cfg.InstallDir, "crash detected (code %d)", res.code)
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
	d, note := core.DefaultInstallDir()
	if note != "" {
		core.Logf(d, "%s", note)
	}
	return d
}
