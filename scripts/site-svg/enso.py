#!/usr/bin/env python3
# Prints the homepage ensō: one open brush stroke as filled outlines, plus the
# centerline the page uses as a reveal mask. Paste the output into
# site/index.html. Run: python3 scripts/site-svg/enso.py
import math
import random

random.seed(7)
CX, CY, R = 200, 200, 150
START, SWEEP = math.radians(296), math.radians(-338)  # counter-clockwise, gap top right
N = 220


def radius(t):
    return R + 5 * math.sin(2 * math.pi * t * 1.3 + 0.4) + 2.5 * math.sin(2 * math.pi * t * 3.1) - 9 * t ** 3


def width(t):
    if t < 0.06:  # the press
        return 26 + 4 * math.sin(t / 0.06 * math.pi)
    if t < 0.78:
        return 26 - 7 * (t - 0.06) / 0.72
    return 19 * (1 - (t - 0.78) / 0.22) ** 1.2 + 1.2


def point(t):
    a = START + SWEEP * t
    r = radius(t)
    return CX + r * math.cos(a), CY + r * math.sin(a)


def normal(t):
    (x0, y0), (x1, y1) = point(max(0, t - 1e-3)), point(min(1, t + 1e-3))
    dx, dy = x1 - x0, y1 - y0
    n = math.hypot(dx, dy)
    return -dy / n, dx / n


def noise(n, amp):
    # Smoothed random walk: a slightly ragged paper edge, not a jitter.
    v, out = 0.0, []
    for _ in range(n):
        v = 0.8 * v + random.uniform(-amp, amp)
        out.append(v)
    return out


def outline(ts, half, edge_amp, cap=False):
    left, right = [], []
    nl, nr = noise(len(ts), edge_amp), noise(len(ts), edge_amp)
    for i, t in enumerate(ts):
        (x, y), (nx, ny), (lo, hi) = point(t), normal(t), half(t)
        left.append((x + nx * (hi + nl[i]), y + ny * (hi + nl[i])))
        right.append((x + nx * (lo - nr[i]), y + ny * (lo - nr[i])))
    pts = left + right[::-1]
    if cap:  # round the head where the brush first pressed down
        (x, y), (nx, ny) = point(ts[0]), normal(ts[0])
        r = (half(ts[0])[1] - half(ts[0])[0]) / 2
        for i in range(1, 16):
            f = math.pi - math.pi * i / 16
            dx, dy = nx * math.cos(f) - ny * math.sin(f) * 1.15, ny * math.cos(f) + nx * math.sin(f) * 1.15
            pts.append((x + dx * r, y + dy * r))
    return "M" + "L".join(f"{x:.1f} {y:.1f}" for x, y in pts) + "Z"


def body():
    ts = [i / N * 0.92 for i in range(N + 1)]

    def half(t):
        w = width(t) * min(1, (0.92 - t) / 0.14) ** 0.7 / 2
        return -w, w
    return outline(ts, half, 0.55, cap=True)


def streaks():
    # Dry-brush gaps inside the body, cut out with fill-rule evenodd.
    out = []
    for t0, t1, off, w in [(0.52, 0.74, 0.16, 0.8), (0.62, 0.86, -0.12, 1.1), (0.70, 0.88, 0.08, 0.7)]:
        ts = [t0 + (t1 - t0) * i / 40 for i in range(41)]

        def half(t, t0=t0, t1=t1, off=off, w=w):
            k = math.sin((t - t0) / (t1 - t0) * math.pi)
            c = off * width(t)
            return c - w * k, c + w * k
        out.append(outline(ts, half, 0.15))
    return "".join(out)


def strands():
    # The tail splits into bristles as the ink runs out.
    out = []
    for off, end, w in [(-0.34, 0.95, 1.5), (-0.1, 1.0, 2.6), (0.16, 0.97, 1.9), (0.36, 0.93, 1.2)]:
        ts = [0.8 + (end - 0.8) * i / 50 for i in range(51)]

        def half(t, off=off, end=end, w=w):
            k = min(1, (t - 0.8) / 0.04) * (1 - (t - 0.8) / (end - 0.8)) ** 0.8
            c = off * max(width(t), 7) * min(1, 0.6 + (t - 0.8) * 8)
            return c - w * k, c + w * k
        out.append(outline(ts, half, 0.2))
    return "".join(out)


def centerline():
    pts = [point(i / 120) for i in range(121)]
    return "M" + "L".join(f"{x:.1f} {y:.1f}" for x, y in pts)


if __name__ == "__main__":
    print(f'<path fill-rule="evenodd" d="{body()}{streaks()}"/>')
    print(f'<path d="{strands()}"/>')
    print(f'<path class="enso-mask" pathLength="1" d="{centerline()}"/>')
