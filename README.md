# RoM2 Launcher (BiffinBoys)

One click for players: download **RoM2 Launcher.exe**, run it, press **PLAY**.
It installs the game the first time, downloads only changed files after that,
and starts `allods2.exe -hat biffinboys.ddns.net`.

## For players
1. Download `RoM2 Launcher.exe` (link on biffinboys.com/rom2) and put it anywhere, e.g. the Desktop.
2. Run it. The game goes to `C:\Allods2` unless you click **Folder** (if `C:\` can't be written to, it uses `%USERPROFILE%\Games\Allods2` and notes why in `.a2launcher\launcher.log`). If the exe is placed inside an existing game folder it uses that folder, and the chosen folder is remembered.

   Note: the BiffinBoys *server* also lives at `C:\Allods2` on the server VM. That is unrelated: players install on their own PCs. Don't run the launcher on the server VM with the default folder, or pick another folder there.
3. Press **PLAY**. First run downloads the whole game; later runs take seconds. The launcher hides while you play and exits when the game closes; if the game crashes it saves a report under `.a2launcher\crash\`.
4. If the game acts strange, press **Repair files**. Your `allods2.cfg` and `ddraw.ini` settings are never overwritten.

Windows SmartScreen may warn the first time (unsigned exe): click *More info → Run anyway*.

## For the server owner (one-time setup)
1. **Cloudflare R2**: dashboard → R2 → *Create bucket* (e.g. `rom2-client`).
   Bucket → Settings → *Custom Domains* → add `files.biffinboys.com`.
2. **API token**: R2 → *Manage API tokens* → create one with *Object Read & Write* for that bucket.
   Note the Account ID, Access Key ID and Secret.
3. **Signing key** (stops anyone who breaks into the web host from pushing files):
   `pip install cryptography` then `python publisher\publish.py keygen C:\rom2keys\launcher.key`.
   Back up that file privately. Put the printed PUBLIC KEY in GitHub → repo → Settings →
   Secrets and variables → Actions → *New secret* `LAUNCHER_PUBKEY`.
4. **Build the launcher**: GitHub → Releases → *Draft a new release*, tag `v1.0.0`, Publish.
   About 2 minutes later `RoM2 Launcher.exe` is attached to the release.

## Publishing a client update
Put the exact client files players should have in one folder (e.g. unpacked `allods2.7z`), then:

```
set R2_ACCOUNT_ID=...  & set R2_ACCESS_KEY_ID=...  & set R2_SECRET_ACCESS_KEY=...  & set R2_BUCKET=rom2-client
python publisher\publish.py build C:\rom2stage publish-out --version 2026.10.13 --news "New maps!" --sign-key C:\rom2keys\launcher.key
python publisher\publish.py upload publish-out            (dry run: shows what would go up)
python publisher\publish.py upload publish-out --apply    (uploads; the file list goes up last)
```
Players get it next time they press PLAY. The server needs the same `world.res` etc.

To push a new launcher version: make a new GitHub release (e.g. `v1.0.1`), download the exe, and add
`--launcher "RoM2 Launcher.exe" --launcher-version 1.0.1` to the build command. Launchers update themselves.

Remove a file from players: `--delete old.dll` (it is moved to the launcher's backup, not destroyed).

## How it works
- `manifest.json` lists every file with size and SHA-256; files are stored by hash under `objects/`,
  so unchanged files are never re-uploaded or re-downloaded. `manifest.json.sig` is an ed25519 signature;
  a launcher built with `LAUNCHER_PUBKEY` refuses unsigned or altered lists.
- Downloads resume, are checksum-verified, and only replace game files once *everything* is downloaded.
  Replaced files go to `<game>\.a2launcher\backup\<time>` (last 3 kept).
- Files in the game folder that are not in the list (saves, screenshots) are left alone.
- Offline: the launcher offers to start the game with the files you have.
- After PLAY the launcher hides and waits for `allods2.exe`. A normal exit closes the launcher silently. A crash (non-zero exit, or the newest new/changed `allods2_*.log` has no `a2mgr closed.` line after its last `started.` line; a new day's log alone is not a crash) writes `.a2launcher\crash\<time>.zip` (exit code, recent `logs\*.log` / a2mgr logs, manifest version, hashes of exe/dll/world.res/ddraw.ini, Windows version — no chr/screenshots/cfg) and asks whether to open that folder.
- Command line (testing): `"RoM2 Launcher.exe" -dir D:\Game -manifest http://... -repair -nolaunch -console`.

## Developers
Go 1.24. `go test ./...`; `./build.sh` cross-compiles the Windows exe (no cgo). The window is plain Win32 + GDI painted by `skin/` in the game's own menu style (skull frame, slider, font1 from graphics.res, icon from allods2.exe); `go run ./cmd/uipreview out.png` renders it anywhere. Regenerate the art with `python tools/gen_ui_assets.py <graphics.res> <allods2.exe>`.
Go was chosen over Python/PyInstaller (antivirus false positives, slow start) and .NET single-file
(bigger, slower CI): one static exe, trivial cross-compile from Linux CI.

## Settings
The Settings button edits display (cnc-ddraw `ddraw.ini`), a2mgr `allods2.cfg` and the game's own options. See [docs/settings.md](docs/settings.md).
