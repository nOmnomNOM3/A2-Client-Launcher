package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
)

func TestSignVerifies(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	dir := t.TempDir()
	exe, sigPath := filepath.Join(dir, "RoM2 Launcher.exe"), filepath.Join(dir, "RoM2.Launcher.exe.sig")
	os.WriteFile(exe, []byte("launcher"), 0o644)
	t.Setenv("LAUNCHER_SIGNING_KEY", base64.StdEncoding.EncodeToString(priv.Seed()))
	t.Setenv("PUBKEY", base64.StdEncoding.EncodeToString(pub))
	if err := run("1.3.1", exe, sigPath); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sigPath)
	sig, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	sha, _ := core.HashFile(exe)
	if !ed25519.Verify(pub, core.LauncherSigMessage("v1.3.1", sha), sig) {
		t.Fatal("signature does not verify")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	t.Setenv("PUBKEY", base64.StdEncoding.EncodeToString(other))
	if err := run("1.3.1", exe, sigPath); err == nil {
		t.Fatal("mismatched key not detected")
	}
}
