# Launcher self-update

Two paths, both end the same way: the new exe is downloaded next to the
running one as `RoM2 Launcher.exe.new`, verified, the running exe is renamed
to `.old` (Windows allows renaming a running exe, not overwriting it), the new
one is moved into place, and the launcher restarts itself with the same
arguments. The next start deletes `.old` / `.new` leftovers.

1. **GitHub releases (v1.3.1+, automatic).** At startup the launcher asks
   `api.github.com/repos/nOmnomNOM3/A2-Client-Launcher/releases/latest`
   (unauthenticated, 5 s timeout; on any failure it just carries on). If the
   tag is a newer semver than its own version, it downloads the
   `RoM2.Launcher.exe` asset. The window shows "Updating launcher…".
2. **File list (all versions).** If the signed `manifest.json` has a
   `launcher` entry (`publish.py build --launcher ... --launcher-version ...`),
   the launcher updates to it when Play/Repair runs. Covered by the manifest
   signature plus the SHA-256 in the entry.

Skipped (with a line in `.a2launcher\launcher.log`): dev builds
(`0.0.0-dev`), draft/pre-releases, a release whose exe is not uploaded yet,
a launcher sitting in a read-only folder such as Program Files, and a restart
right after an update whose exe still reports the old version (loop guard).

## Integrity of GitHub updates

- **Signed (preferred).** If the repo secret `LAUNCHER_SIGNING_KEY` holds the
  publisher's private key file contents (the same key as `--sign-key`, whose
  public half is `LAUNCHER_PUBKEY`), `release.yml` uploads
  `RoM2.Launcher.exe.sig`: an ed25519 signature over
  `"a2launcher-selfupdate\n<version>\n<sha256>\n"`. The launcher verifies it
  with its embedded public key and refuses the update if it fails. Binding
  the version stops an old signed exe being replayed as a newer release.
  Builds made with the secret present also get `REQUIRE_LAUNCHER_SIG=1`, so
  from then on they ignore any release without a valid signature. A stolen
  GitHub login or token alone can't push a launcher to players.
- **SHA-256 only (fallback, no secret).** The launcher checks the SHA-256
  that GitHub reports for the asset (`digest` in the API), or the
  `RoM2.Launcher.exe.sha256` asset. That catches corruption and tampering
  in transit (both come over HTTPS from GitHub), but anyone who can publish a
  release on the repo can publish a matching hash.

If you add the secret later, the first signed release still installs on
older unsigned-mode launchers (they check the digest too), and launchers
built after that require signatures. If you ever remove the secret, launchers
built with it will stop auto-updating (they log why).

## Testing

1. Install a launcher built from a release (e.g. v1.3.1) in a writable folder.
2. Publish a newer release (v1.3.2). Wait for CI to attach the exe.
3. Start the old launcher: "Updating launcher…", it restarts, and the window
   now shows v1.3.2. `launcher.log` has `self-update: 1.3.1 -> v1.3.2 ...`.
