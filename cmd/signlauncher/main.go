// Command signlauncher signs a launcher exe for GitHub self-update.
//
//	LAUNCHER_SIGNING_KEY=<base64 key> signlauncher -version 1.3.1 -in "dist/RoM2 Launcher.exe" -out dist/RoM2.Launcher.exe.sig
//
// The key is the same base64 ed25519 private key file the publisher uses
// (publish.py keygen / --sign-key). It writes the base64 signature over
// core.LauncherSigMessage(version, sha256(exe)). If PUBKEY is set, the
// signature is checked against it so a mismatched key fails the build.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
)

func main() {
	ver := flag.String("version", "", "release version (tag without v)")
	in := flag.String("in", "", "exe to sign")
	out := flag.String("out", "", "signature file to write")
	flag.Parse()
	if err := run(*ver, *in, *out); err != nil {
		fmt.Fprintln(os.Stderr, "signlauncher:", err)
		os.Exit(1)
	}
}

func run(ver, in, out string) error {
	if ver == "" || in == "" || out == "" {
		return fmt.Errorf("need -version, -in and -out")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("LAUNCHER_SIGNING_KEY")))
	if err != nil {
		return fmt.Errorf("LAUNCHER_SIGNING_KEY is not base64: %v", err)
	}
	var key ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		key = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		key = ed25519.PrivateKey(raw)
	default:
		return fmt.Errorf("LAUNCHER_SIGNING_KEY has %d bytes, want 32", len(raw))
	}
	sha, err := core.HashFile(in)
	if err != nil {
		return err
	}
	msg := core.LauncherSigMessage(ver, sha)
	sig := ed25519.Sign(key, msg)
	if p := strings.TrimSpace(os.Getenv("PUBKEY")); p != "" {
		pub, err := base64.StdEncoding.DecodeString(p)
		if err != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, msg, sig) {
			return fmt.Errorf("LAUNCHER_SIGNING_KEY does not match LAUNCHER_PUBKEY")
		}
	}
	fmt.Printf("signed %s (version %s, sha256 %s)\n", in, ver, sha)
	return os.WriteFile(out, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}
