#!/usr/bin/env python3
"""Publish the RoM2 client for the launcher.

  build   STAGE OUT   hash a staged client folder -> OUT/manifest.json + OUT/objects/<sha256>
  upload  OUT         copy OUT to Cloudflare R2 (S3 API). Dry run unless --apply.
  keygen  KEYFILE     make a signing key (keep KEYFILE private; prints the public key for the launcher build)

Upload needs env vars R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET.
Only Python's standard library is needed, except signing (pip install cryptography).
"""
import argparse, zipfile, base64, datetime, hashlib, hmac, json, os, shutil, sys, urllib.error, urllib.parse, urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

SKIP_NAMES = {"thumbs.db", "desktop.ini", ".ds_store"}
SKIP_DIRS = {".a2launcher", ".git", "logs", "screenshots"}
SKIP_EXTS = (".lnk", ".url", ".log", ".tmp", ".a2c")  # .a2c = per-player character cache
# Files allods2.exe / a2mgr.dll open by name. The exe mounts the .res archives
# directly; a missing speech/video/music.res let the game start but crashed it
# on entering a map (2026-10-15). graphics.res is not opened by this exe (it
# reads data/graphics), so it is not required.
REQUIRED_FILES = ["allods2.exe", "a2mgr.dll", "ddraw.dll", "ddraw.ini", "smackw32.dll",
                  "scenario.dll", "main.res", "patch.res", "world.res", "locale.res",
                  "scenario.res", "sfx.res", "movies.res", "speech.res", "video.res",
                  "music.res"]
# Player-owned files: installed once, never overwritten.
DEFAULT_ONCE = ["allods2.cfg", "ddraw.ini"]


def sha256_file(p):
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()


PACK_TARGET = 64 * 1024 * 1024
ZIP_DATE = (2020, 1, 1, 0, 0, 0)  # fixed, so identical content gives an identical pack


def build_packs(stage, files, objdir, target=PACK_TARGET):
    """Group files (sorted by path) into ~target-byte zips stored as objects/<sha256>.
    Files bigger than target are left out (per-file download is as fast)."""
    groups, cur, size = [], [], 0
    for e in sorted(files, key=lambda x: x["path"].lower()):
        if e["size"] >= target:
            continue
        if cur and size + e["size"] > target:
            groups.append(cur); cur, size = [], 0
        cur.append(e); size += e["size"]
    if len(cur) > 1:
        groups.append(cur)
    packs, new = [], 0
    for g in groups:
        tmp = os.path.join(objdir, "pack.tmp")
        with zipfile.ZipFile(tmp, "w", zipfile.ZIP_DEFLATED, compresslevel=6) as z:
            for e in g:
                zi = zipfile.ZipInfo(e["path"], ZIP_DATE)
                zi.compress_type = zipfile.ZIP_DEFLATED
                zi.external_attr = 0o644 << 16
                with open(os.path.join(stage, e["path"]), "rb") as src, z.open(zi, "w") as dst:
                    shutil.copyfileobj(src, dst, 1 << 20)
        sha = sha256_file(tmp)
        dst = os.path.join(objdir, sha)
        if os.path.exists(dst):
            os.remove(tmp)
        else:
            os.replace(tmp, dst); new += 1
        packs.append({"sha256": sha, "size": os.path.getsize(dst), "files": [e["path"] for e in g]})
    return packs, new


def cmd_build(a):
    stage = os.path.abspath(a.stage)
    if not os.path.isfile(os.path.join(stage, a.exe)):
        sys.exit(f"FAIL {a.exe} not found in {stage}")
    once = {x.lower() for x in (a.once if a.once is not None else DEFAULT_ONCE)}
    objdir = os.path.join(a.out, "objects")
    os.makedirs(objdir, exist_ok=True)
    files, seen, new = [], set(), 0
    for root, dirs, names in os.walk(stage):
        dirs[:] = sorted(d for d in dirs if d.lower() not in SKIP_DIRS)
        for n in sorted(names):
            if n.lower() in SKIP_NAMES or n.lower().endswith(SKIP_EXTS):
                print(f"SKIP {os.path.relpath(os.path.join(root, n), stage)}")
                continue
            full = os.path.join(root, n)
            rel = os.path.relpath(full, stage).replace(os.sep, "/")
            if rel.lower() in seen:
                sys.exit(f"FAIL two files differ only by case: {rel}")
            seen.add(rel.lower())
            sha = sha256_file(full)
            e = {"path": rel, "size": os.path.getsize(full), "sha256": sha}
            if rel.lower() in once:
                e["once"] = True
            files.append(e)
            dst = os.path.join(objdir, sha)
            if not os.path.exists(dst):
                shutil.copyfile(full, dst)
                new += 1
    missing = [r for r in REQUIRED_FILES if r.lower() not in seen]
    if missing:
        msg = "required game files missing from " + stage + ": " + ", ".join(missing)
        if not a.allow_missing:
            sys.exit("FAIL " + msg + "\n     (copy them from the full client, or pass --allow-missing to publish anyway)")
        print("WARN " + msg)
    packs, pnew = ([], 0) if a.no_packs else build_packs(stage, files, objdir)
    new += pnew
    m = {"format": 1, "version": a.version, "news": a.news, "exe": a.exe,
         "args": ["-hat", a.hat], "objects": "objects/", "files": files}
    if packs:
        m["packs"] = packs
    if a.delete:
        m["delete"] = a.delete
    if a.launcher:
        sha = sha256_file(a.launcher)
        shutil.copyfile(a.launcher, os.path.join(objdir, sha))
        m["launcher"] = {"version": a.launcher_version, "url": "objects/" + sha,
                         "sha256": sha, "size": os.path.getsize(a.launcher)}
        if not a.launcher_version:
            sys.exit("FAIL --launcher needs --launcher-version")
    data = json.dumps(m, indent=1).encode()
    with open(os.path.join(a.out, "manifest.json"), "wb") as f:
        f.write(data)
    if a.sign_key:
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        key = Ed25519PrivateKey.from_private_bytes(base64.b64decode(open(a.sign_key).read().strip()))
        with open(os.path.join(a.out, "manifest.json.sig"), "w") as f:
            f.write(base64.b64encode(key.sign(data)).decode())
    total = sum(x["size"] for x in files)
    print(f"OK  {len(files)} files, {total/1e6:.1f} MB, {len(packs)} packs, {new} new objects, version {a.version}"
          + (", signed" if a.sign_key else ", NOT signed"))
    big = [x["path"] for x in files if x["size"] > 25_000_000]
    if big:
        print(f"NOTE {len(big)} file(s) over 25 MB ({', '.join(big[:3])}): use R2, not Cloudflare Pages")


def cmd_keygen(a):
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives import serialization as s
    if os.path.exists(a.keyfile):
        sys.exit(f"FAIL {a.keyfile} exists; not overwriting")
    k = Ed25519PrivateKey.generate()
    raw = k.private_bytes(s.Encoding.Raw, s.PrivateFormat.Raw, s.NoEncryption())
    pub = k.public_key().public_bytes(s.Encoding.Raw, s.PublicFormat.Raw)
    with open(a.keyfile, "w") as f:
        f.write(base64.b64encode(raw).decode())
    print("OK  private key saved to", a.keyfile, "(back it up, never share it)")
    print("PUBLIC KEY (GitHub secret LAUNCHER_PUBKEY):", base64.b64encode(pub).decode())


# --- minimal S3 SigV4 client for R2 -------------------------------------
class R2:
    def __init__(self):
        try:
            self.acct = os.environ["R2_ACCOUNT_ID"]; self.ak = os.environ["R2_ACCESS_KEY_ID"]
            self.sk = os.environ["R2_SECRET_ACCESS_KEY"]; self.bucket = os.environ["R2_BUCKET"]
        except KeyError as e:
            sys.exit(f"FAIL environment variable {e.args[0]} is not set")
        self.host = f"{self.acct}.r2.cloudflarestorage.com"

    def req(self, method, key, body=b"", headers=None):
        path = "/" + self.bucket + "/" + urllib.parse.quote(key)
        now = datetime.datetime.now(datetime.timezone.utc)
        amz, day = now.strftime("%Y%m%dT%H%M%SZ"), now.strftime("%Y%m%d")
        ph = hashlib.sha256(body).hexdigest()
        h = {"host": self.host, "x-amz-content-sha256": ph, "x-amz-date": amz}
        h.update({k.lower(): v for k, v in (headers or {}).items()})
        sh = ";".join(sorted(h))
        creq = "\n".join([method, path, "", "".join(f"{k}:{h[k]}\n" for k in sorted(h)), sh, ph])
        scope = f"{day}/auto/s3/aws4_request"
        sts = "\n".join(["AWS4-HMAC-SHA256", amz, scope, hashlib.sha256(creq.encode()).hexdigest()])
        k = ("AWS4" + self.sk).encode()
        for part in (day, "auto", "s3", "aws4_request"):
            k = hmac.new(k, part.encode(), hashlib.sha256).digest()
        sig = hmac.new(k, sts.encode(), hashlib.sha256).hexdigest()
        h["authorization"] = f"AWS4-HMAC-SHA256 Credential={self.ak}/{scope}, SignedHeaders={sh}, Signature={sig}"
        r = urllib.request.Request("https://" + self.host + path, data=body if method == "PUT" else None,
                                   method=method, headers=h)
        try:
            with urllib.request.urlopen(r, timeout=600) as resp:
                return resp.status
        except urllib.error.HTTPError as e:
            if method == "HEAD" and e.code == 404:
                return 404
            sys.exit(f"FAIL R2 {method} {key}: {e.code} {e.read()[:300]!r}")


def filter_missing(r2, prefix, shas, workers=16, progress_every=500):
    """HEAD-check objects in parallel; return shas that are not already on R2."""
    shas = sorted(shas)
    total = len(shas)
    todo, exist = [], 0
    done = 0

    def check(sha):
        return sha, r2.req("HEAD", prefix + "objects/" + sha) == 200

    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [pool.submit(check, sha) for sha in shas]
        for fut in as_completed(futures):
            sha, exists = fut.result()
            done += 1
            if exists:
                exist += 1
            else:
                todo.append(sha)
            if done % progress_every == 0 or done == total:
                print(f"checking {done}/{total}...", flush=True)
    print(f"check done: {exist} already on R2, {len(todo)} to upload", flush=True)
    return sorted(todo)


MANIFEST_CC = "no-cache, max-age=0, must-revalidate"


def cmd_upload(a):
    man = os.path.join(a.out, "manifest.json")
    m = json.load(open(man))
    shas = {f["sha256"] for f in m["files"]}
    shas |= {p["sha256"] for p in m.get("packs", [])}
    if m.get("launcher"):
        shas.add(m["launcher"]["sha256"])
    prefix = a.prefix.strip("/") + "/"
    if not a.apply:
        size = sum(os.path.getsize(os.path.join(a.out, "objects", s)) for s in shas)
        print(f"DRY {len(shas)} objects (up to {size/1e6:.1f} MB; already-uploaded ones are skipped with --apply)"
              f" + manifest -> {prefix}. Add --apply to upload.")
        return
    r2 = R2()
    todo = filter_missing(r2, prefix, shas)
    size = sum(os.path.getsize(os.path.join(a.out, "objects", s)) for s in todo)
    for i, sha in enumerate(todo, 1):
        body = open(os.path.join(a.out, "objects", sha), "rb").read()
        r2.req("PUT", prefix + "objects/" + sha, body, {"cache-control": "public, max-age=31536000, immutable"})
        print(f"  {i}/{len(todo)} {sha[:12]} {len(body)/1e6:.1f} MB")
    # manifest last, so players never see a list pointing at missing files
    sig = man + ".sig"
    if os.path.exists(sig):
        r2.req("PUT", prefix + "manifest.json.sig", open(sig, "rb").read(), {"cache-control": MANIFEST_CC})
    r2.req("PUT", prefix + "manifest.json", open(man, "rb").read(),
           {"cache-control": MANIFEST_CC, "content-type": "application/json"})
    print(f"OK  uploaded {len(todo)} new objects ({size/1e6:.1f} MB) + manifest, version {m.get('version')}")


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sp = p.add_subparsers(dest="cmd", required=True)
    b = sp.add_parser("build"); b.add_argument("stage"); b.add_argument("out")
    b.add_argument("--version", required=True); b.add_argument("--news", default="")
    b.add_argument("--exe", default="allods2.exe"); b.add_argument("--hat", default="biffinboys.ddns.net")
    b.add_argument("--once", nargs="*", help="player-owned files (default: allods2.cfg ddraw.ini)")
    b.add_argument("--delete", nargs="*", help="files players should no longer have")
    b.add_argument("--launcher", help="new launcher exe for self-update"); b.add_argument("--launcher-version")
    b.add_argument("--sign-key"); b.add_argument("--no-packs", action="store_true", help="skip pack archives")
    b.add_argument("--allow-missing", action="store_true", help="publish even if REQUIRED_FILES are missing")
    u = sp.add_parser("upload"); u.add_argument("out"); u.add_argument("--prefix", default="rom2/client")
    u.add_argument("--apply", action="store_true")
    k = sp.add_parser("keygen"); k.add_argument("keyfile")
    a = p.parse_args()
    {"build": cmd_build, "upload": cmd_upload, "keygen": cmd_keygen}[a.cmd](a)


if __name__ == "__main__":
    main()
