# A solid-colour PNG (no PIL on a runner): python3 -I solid.py <out.png> <w> <h> <r> <g> <b>
import struct, sys, zlib
out, w, h, r, g, b = sys.argv[1], *map(int, sys.argv[2:7])
row = b"\x00" + bytes((r, g, b)) * w
def chunk(t, d):
    return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(row * h, 9)) + chunk(b"IEND", b"")
open(out, "wb").write(png)
