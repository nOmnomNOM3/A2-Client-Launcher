# RoM2 online client: minimum file set (igroglaz/a2 base + kit)

Worked out from the PE imports, the strings in allods2.exe and a2mgr.dll (kit build), and the
igroglaz/a2 tree. **Not run under Wine**: the box has no Wine, so nothing was confirmed by tracing
file access. Test the staged folder once on Windows before publishing.

## Required (~160 MB without music)
| File / folder | Why |
|---|---|
| `allods2.exe` | the game |
| `a2mgr.dll` (**kit version**) | imported by the exe; includes the map auto-download fix; the Hat checks its CRC |
| `scenario.dll` | exe loads it at runtime ("Can't find scenario.dll") |
| `smackw32.dll` | imported by the exe (videos and shop animations) |
| `SDL.dll`, `SDL_image.dll` | imported by a2mgr |
| `libpng12-0.dll`, `zlib1.dll`, `jpeg.dll`, `libtiff-3.dll` | loaded by SDL_image (menu PNGs) |
| `ddraw.dll`, `ddraw.ini` | cnc-ddraw (from the kit; not in igroglaz/a2). ddraw.ini is shipped once and never overwritten |
| `world.res` (**kit version**) | game data matching the servers |
| `locale.res`, `patch.res`, `patch_en.res` | opened by the exe and a2mgr |
| `data\graphics\`, `data\sfx\`, `data\locale\`, `data\main\`, `data\scenario\`, `data\world\` | unpacked resources; this client reads `data\…` instead of GOG's graphics.res, sfx.res and so on |
| `data\movies\` (5 MB) | shop and inn animations, which aren't intro videos |
| `data\video\` (tiny .reg files), `data\version`, `data\version_id` | small; the exe refers to `/allods/version` and `version_id` |

Windows system DLLs (ddraw, dsound, wsock32, wininet, msacm32, comctl32, dbghelp…) come with
Windows and Wine, so nothing needs shipping. No MSVC runtime is needed apart from msvcrt.dll, which
is part of Windows. No binkw32.

## Optional
- `data\music\` (134 MB). The game runs without it; `-nomusic` is also an option. Recommend shipping it.
- `maps\*.alm`. The server sends maps, and the kit's a2mgr saves downloaded maps into the client folder.
  Shipping your server maps up front just skips the first download (and the old kids3-style problem).

## Player-owned: never overwrite
- `allods2.cfg`: ship a default; the launcher installs it only when it's missing (publish.py `once`, the default).
- `ddraw.ini`: same rule.
- `chr\` (local character files `*.a2c` and lockflag), `screenshots\`, `logs\`, `*.log`, `famehall.dat`.
- `*.alm` files in the client root that a2mgr downloaded.
  The launcher must not delete files it doesn't recognise.

## Don't ship (junk for players)
`rom2me.exe`, `rom2me.dll` (map editor), `engine32.dll`, `helper32.dll`, `SDL_ttf.dll`, `libfreetype-6.dll`
(nothing in the exe or a2mgr loads them), `BES_1.7.9\` and `_allods2_CPU_limit.bat` (CPU limiter),
`update.bat` (git updater), `templates.bin`, `Description Checks.ini`, `Description Instants.ini` (server-side),
`Music.ini`, `patch25*.txt`, `.gitattributes`, `*.bak*`, `*.orig`, and any GOG `.res` files or GOG and installer leftovers.
If a dropped DLL turns out to be needed in the Windows test, add it to `REQUIRED_FILES` in the script.

## Staging
```
python tools\stage_client.py "C:\messy\Allods2" C:\rom2stage            (dry run, lists copy/skip/missing)
python tools\stage_client.py "C:\messy\Allods2" C:\rom2stage --apply    (--no-music / --no-maps / --no-default-cfg)
```
Then copy the kit's `a2mgr.dll`, `world.res` and cnc-ddraw files over the staged ones if the messy folder had older copies.
Exit code 2 means a required file is missing.
