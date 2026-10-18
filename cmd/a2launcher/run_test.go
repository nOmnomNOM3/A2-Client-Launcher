//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
)

func fakeGame(t *testing.T, script string) *session {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "allods2.exe"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &session{cfg: core.Config{InstallDir: dir}, manifest: &core.Manifest{Exe: "allods2.exe", Args: []string{"-hat", "biffinboys.ddns.net"}}}
}

func TestStartGameCwdAndArgs(t *testing.T) {
	s := fakeGame(t, "pwd > cwd.txt; echo \"$@\" > args.txt\n")
	r, err := s.startGame()
	if err != nil {
		t.Fatal(err)
	}
	exited, res, err := s.earlyCheck(r)
	if err != nil || !exited || res.code != 0 {
		t.Fatalf("exited=%v res=%+v err=%v", exited, res, err)
	}
	cwd, _ := os.ReadFile(filepath.Join(s.cfg.InstallDir, "cwd.txt"))
	want, _ := filepath.EvalSymlinks(s.cfg.InstallDir)
	got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(cwd)))
	if got != want {
		t.Fatalf("cwd %q want %q", got, want)
	}
	args, _ := os.ReadFile(filepath.Join(s.cfg.InstallDir, "args.txt"))
	if strings.TrimSpace(string(args)) != "-hat biffinboys.ddns.net" {
		t.Fatalf("args %q", args)
	}
	log, _ := os.ReadFile(core.LogPath(s.cfg.InstallDir))
	if !strings.Contains(string(log), "biffinboys.ddns.net") || !strings.Contains(string(log), "game exited: code=0") {
		t.Fatalf("log missing steps:\n%s", log)
	}
}

func TestEarlyNonZeroExitIsError(t *testing.T) {
	s := fakeGame(t, "exit 3\n")
	r, err := s.startGame()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.earlyCheck(r)
	if err == nil || !strings.Contains(err.Error(), "exit code 3") {
		t.Fatalf("want exit code error, got %v", err)
	}
}

func TestStillRunningPassesEarlyCheck(t *testing.T) {
	old := earlyExitWindow
	earlyExitWindow = 200 * time.Millisecond
	defer func() { earlyExitWindow = old }()
	s := fakeGame(t, "sleep 1\n")
	r, err := s.startGame()
	if err != nil {
		t.Fatal(err)
	}
	if exited, _, err := s.earlyCheck(r); exited || err != nil {
		t.Fatalf("exited=%v err=%v", exited, err)
	}
	if _, err := s.watchGame(r, false, exitResult{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMissingExeAndRelativeDir(t *testing.T) {
	s := &session{cfg: core.Config{InstallDir: "rel-" + t.Name()}}
	defer os.RemoveAll(s.cfg.InstallDir)
	_, exe, _, _ := s.gamePaths()
	if !filepath.IsAbs(exe) {
		t.Fatalf("exe not absolute: %s", exe)
	}
	if _, err := s.startGame(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("got %v", err)
	}
}

func TestStartGameClearsCharCache(t *testing.T) {
	s := fakeGame(t, "ls > files.txt\n")
	dir := s.cfg.InstallDir
	for _, f := range []string{"311777688826776.a2c", "famehall.dat", "hotkey.1.a2key"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	r, err := s.startGame()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.earlyCheck(r); err != nil {
		t.Fatal(err)
	}
	seen, _ := os.ReadFile(filepath.Join(dir, "files.txt"))
	if strings.Contains(string(seen), ".a2c") {
		t.Fatalf("game saw a cache file:\n%s", seen)
	}
	for _, f := range []string{"famehall.dat", "hotkey.1.a2key"} {
		if !strings.Contains(string(seen), f) {
			t.Fatalf("%s missing at launch:\n%s", f, seen)
		}
	}
}
