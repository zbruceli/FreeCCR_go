#!/usr/bin/env python3
# FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
# Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.
"""Time the numpy reference kernels (the exact FreeCCR math) on a 2000x1333
image, for an apples-to-apples comparison against the Go benchmarks. numpy is
SIMD-vectorized and effectively single-threaded for these elementwise ops."""
import time
import numpy as np
import gen_golden as g

W, H = 2000, 1333
REPS = 15


def bench(name, fn):
    fn()  # warmup
    t = []
    for _ in range(REPS):
        s = time.perf_counter()
        fn()
        t.append(time.perf_counter() - s)
    print(f"{name:28s} {1000*np.median(t):8.1f} ms/frame  (numpy, 1 core+SIMD)")


def main():
    rs = np.random.RandomState(7)
    img_u16 = rs.randint(1, 65536, (H, W, 3)).astype(np.uint16)
    img_f = img_u16.astype(np.float32)

    combo = dict(kelvin_shift=30.0, tint_shift=-20.0, exposure=40.0,
                 brightness=15.0, highlights=30.0, shadows=25.0,
                 blackpoint=20.0, whitepoint=-15.0, contrast=35.0,
                 saturation=40.0, sub_saturation=25.0,
                 ch_input_gain=10.0, ch_master_gain=8.0, ch_r_gain=12.0)
    black = [61000.0, 58000.0, 52000.0]
    white = [9150.0, 8700.0, 7800.0]
    p_lo = np.array([800.0, 900.0, 1000.0])
    p_hi = np.array([64000.0, 64200.0, 64500.0])
    odf = np.array([1.0, 1.05, 0.95])

    print(f"image {W}x{H} ({W*H/1e6:.2f} MP), median of {REPS}")
    bench("adjust_image (full combo)", lambda: g.adjust_image(img_u16, **combo))
    bench("twopoint_invert density ws", lambda: g.twopoint_invert(img_f, black, white, True, True))
    bench("apply_reference_normalization",
          lambda: g.apply_reference_normalization(img_f, p_lo, p_hi, odf))


if __name__ == "__main__":
    main()
