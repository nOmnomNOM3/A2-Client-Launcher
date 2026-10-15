#!/usr/bin/env python3
"""Copy only the files a Rage of Mages 2 online client needs from a messy folder
into a clean staging folder for publisher/publish.py.

  python tools/stage_client.py <messy client folder> <staging folder>            (dry run)
  python tools/stage_client.py <src> <dst> --apply [--no-music] [--no-maps]

Dry run by default: lists what would be copied, what is skipped, and any missing
required files (exit code 2 if something required is missing).
Names are matched case-insensitively (Windows folders). Source is never modified.
See client_minimum.md for the reasoning behind each entry.
"""
import argparse, os, shutil, sys

# Required single files (top level).
REQUIRED_FILES = [
    "allods2.exe", "a2mgr.dll",             # game + patched a2mgr (map auto-download fix)
    "scenario.dll", "smackw32.dll",          # loaded by the exe
    "SDL.dll", "SDL_image.dll",              # imported by a2mgr
    "libpng12-0.dll", "zlib1.dll", "jpeg.dll", "libtiff-3.dll",  # loaded by SDL_image
    "ddraw.dll", "ddraw.ini",                # cnc-ddraw
    "world.res", "locale.res", "patch.res", "patch_en.res",
]
# Required folders (copied recursively).
REQUIRED_DIRS = [
    "data/graphics", "data/sfx", "data/locale", "data/main", "data/scenario",
    "data/world", "data/movies", "data/video",
]
REQUIRED_DATA_FILES = ["data/version", "data/version_id"]
# Optional.
OPTIONAL_DIRS = {"music": ["data/music"], "maps": ["maps"]}
# Player-owned defaults: shipped, but the launcher installs them only if missing (publish.py --once).
DEFAULT_FILES = ["allods2.cfg"]


def index(root):
    """lower-case relative path -> real relative path, for case-insensitive matching."""
    m = {}
    for d, _, files in os.walk(root):
        for f in files:
            rel = os.path.relpath(os.path.join(d, f), root).replace(os.sep, "/")
            m[rel.lower()] = rel
    return m


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("src"); ap.add_argument("dst")
    ap.add_argument("--apply", action="store_true", help="actually copy")
    ap.add_argument("--no-music", action="store_true", help="skip data/music (134 MB)")
    ap.add_argument("--no-maps", action="store_true", help="skip maps/ (server sends maps)")
    ap.add_argument("--no-default-cfg", action="store_true", help="do not ship allods2.cfg")
    a = ap.parse_args()

    idx = index(a.src)
    pick, missing = [], []

    def want_file(rel, required):
        real = idx.get(rel.lower())
        if real: pick.append(real)
        elif required: missing.append(rel)

    def want_dir(rel, required):
        pre = rel.lower().rstrip("/") + "/"
        hits = [v for k, v in idx.items() if k.startswith(pre)]
        if hits: pick.extend(hits)
        elif required: missing.append(rel + "/")

    for f in REQUIRED_FILES + REQUIRED_DATA_FILES: want_file(f, True)
    for d in REQUIRED_DIRS: want_dir(d, True)
    if not a.no_music: [want_dir(d, False) for d in OPTIONAL_DIRS["music"]]
    if not a.no_maps: [want_dir(d, False) for d in OPTIONAL_DIRS["maps"]]
    if not a.no_default_cfg: [want_file(f, False) for f in DEFAULT_FILES]

    picked = set(pick)
    skipped = sorted(v for v in idx.values() if v not in picked)
    size = sum(os.path.getsize(os.path.join(a.src, p)) for p in picked)
    print(f"copy: {len(picked)} files, {size/1e6:.1f} MB")
    tops = {}
    for s in skipped:
        t = s.split("/")[0] + ("/" if "/" in s else "")
        tops[t] = tops.get(t, 0) + 1
    print(f"skip: {len(skipped)} files")
    for t, n in sorted(tops.items()):
        print(f"   {t}  ({n})" if n > 1 else f"   {t}")
    if missing:
        print("MISSING REQUIRED:")
        for m in missing: print("   " + m)

    if a.apply:
        for rel in sorted(picked):
            out = os.path.join(a.dst, rel)
            os.makedirs(os.path.dirname(out) or ".", exist_ok=True)
            shutil.copy2(os.path.join(a.src, rel), out)
        print(f"copied into {a.dst}")
    else:
        print("dry run: nothing copied (add --apply)")
    sys.exit(2 if missing else 0)


if __name__ == "__main__":
    main()
