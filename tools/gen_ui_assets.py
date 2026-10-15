#!/usr/bin/env python3
"""Extract the launcher skin from the game's own files into skin/assets/.

Usage: python tools/gen_ui_assets.py <graphics.res or unpacked graphics dir> <allods2.exe>

Writes (all embedded into the launcher exe):
  frame_0..8.png  interface/lm.256 frames 0-8: background tile, skull corners and edges
  bar_cap.png, bar_track.png, bar_knob.png  interface/scrlbars.256 frames 2, 7, 4 (slider)
  font.png + font.txt  font1/font1.16 (+ .dat widths): 16x15 glyphs, chars 32-255, 16 per row
  ../../cmd/a2launcher/game.ico  icon group 1 of allods2.exe
Needs Pillow (and pefile for the icon)."""
import os, struct, sys
from PIL import Image

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'skin', 'assets')

def res_files(path):
    """name -> bytes, from a '&YA1' .res archive or an unpacked folder."""
    if os.path.isdir(path):
        class D(dict):
            def __missing__(s, k): return open(os.path.join(path, *k.split('/')), 'rb').read()
        return D()
    d = open(path, 'rb').read()
    magic, _, _, _, toff, cnt = struct.unpack_from('<4s5I', d, 0)
    if magic != b'&YA1': raise SystemExit('not a .res archive')
    recs = []
    for i in range(cnt):
        _, a, b, isdir = struct.unpack_from('<4I', d, toff + 32 * i)
        recs.append((a, b, isdir, d[toff + 32 * i + 16:toff + 32 * i + 32].split(b'\0')[0].decode('latin1')))
    parent = {}
    for i, (a, b, isdir, _) in enumerate(recs):
        if isdir:
            for c in range(a, a + b): parent[c] = i
    out = {}
    for i, (a, b, isdir, name) in enumerate(recs):
        if isdir: continue
        p, j = [name], i
        while j in parent: j = parent[j]; p.append(recs[j][3])
        out['/'.join(reversed(p)).lower()] = d[a:a + b]
    return out

def read256(d):
    """.256: 256 BGRx palette, then frames (w, h, size, RLE). Index 0 = transparent."""
    pal = [(d[i*4+2], d[i*4+1], d[i*4]) for i in range(256)]
    p, frames = 1024, []
    while p + 12 <= len(d):
        w, h, sz = struct.unpack_from('<3i', d, p)
        if w <= 0 or h <= 0 or sz < 0: break
        q, end = p + 12, p + 12 + sz; px = bytearray(w*h); y = 0
        while y < h and q < end:
            b = d[q]
            if b & 0x40: y += b & 0x3f; q += 1; continue
            x = 0
            while x < w and q < end:
                b = d[q]; q += 1
                if b & 0x80: x += b & 0x3f; continue
                px[y*w+x:y*w+x+b] = d[q:q+b]; q += b; x += b
            y += 1
        im = Image.new('RGBA', (w, h))
        im.putdata([(0, 0, 0, 0) if v == 0 else pal[v] + (255,) for v in px])
        frames.append(im); p = end
    return frames

def read16(d):
    """.16 font: frames (w, h, size, RLE); literal runs are bytes of two 4-bit alpha pixels, low nibble first."""
    p, frames = 0, []
    while p + 12 <= len(d):
        w, h, sz = struct.unpack_from('<3i', d, p)
        if w <= 0 or h <= 0 or sz < 0 or w > 64: break
        q, end = p + 12, p + 12 + sz; px = bytearray(w*h); y = 0
        while y < h and q < end:
            b = d[q]
            if b & 0x40: y += b & 0x3f; q += 1; continue
            x = 0
            while x < w and q < end:
                b = d[q]; q += 1
                if b & 0x80: x += b & 0x3f; continue
                for i in range(b):
                    v = d[q+i]
                    for k, t in enumerate((v & 15, v >> 4)):
                        if x + 2*i + k < w: px[y*w + x + 2*i + k] = t * 17
                q += b; x += 2*b
            y += 1
        frames.append(Image.frombytes('L', (w, h), bytes(px))); p = end
    return frames

def icon(exe, out):
    import pefile
    pe = pefile.PE(exe); ico = {}; grp = []
    for t in pe.DIRECTORY_ENTRY_RESOURCE.entries:
        for e in t.directory.entries:
            for l in e.directory.entries:
                data = pe.get_data(l.data.struct.OffsetToData, l.data.struct.Size)
                if t.id == 3: ico[e.id] = data
                if t.id == 14: grp.append(data)
    g = grp[0]; n = struct.unpack_from('<H', g, 4)[0]
    head = bytearray(struct.pack('<3H', 0, 1, n)); body = b''; off = 6 + 16*n
    for i in range(n):
        w, h, c, r, pl, bc, sz, rid = struct.unpack_from('<4B2HIH', g, 6 + 14*i)
        head += struct.pack('<4B2H2I', w, h, c, r, pl, bc, sz, off + len(body)); body += ico[rid]
    open(out, 'wb').write(head + body)

def main():
    res, exe = sys.argv[1], sys.argv[2]
    f = res_files(res); os.makedirs(OUT, exist_ok=True)
    lm = read256(f['interface/lm.256'])
    for i in range(9): lm[i].save(os.path.join(OUT, f'frame_{i}.png'), optimize=True)
    sb = read256(f['interface/scrlbars.256'])
    for name, i in (('bar_cap', 2), ('bar_track', 7), ('bar_knob', 4)): sb[i].save(os.path.join(OUT, name + '.png'), optimize=True)
    glyphs = read16(f['font1/font1.16']); widths = struct.unpack('<224i', f['font1/font1.dat'][:896])
    atlas = Image.new('L', (16*16, 15*14))
    for i, g in enumerate(glyphs[:224]): atlas.paste(g, ((i % 16) * 16, (i // 16) * 15))
    atlas.save(os.path.join(OUT, 'font.png'), optimize=True)
    open(os.path.join(OUT, 'font.txt'), 'w').write(' '.join(map(str, widths)) + '\n')
    icon(exe, os.path.join(OUT, '..', '..', 'cmd', 'a2launcher', 'game.ico'))
    print('wrote', os.path.abspath(OUT))

if __name__ == '__main__': main()
