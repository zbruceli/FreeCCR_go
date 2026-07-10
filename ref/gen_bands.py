#!/usr/bin/env python3
# FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
# Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.
"""Golden fixtures for color bands, generated with the REAL FreeCCR cv2 kernels
(apply_color_band_adjustments). Bands need cv2, so run this in the A/B venv:

    /tmp/ccrvenv/bin/python ref/gen_bands.py <FreeCCR/src>
"""
import json
import os
import sys

import numpy as np

COLORS = ("red", "skin", "yellow", "green", "cyan", "blue", "purple")
PARAMS = ("subsat", "sat", "bright", "hue")


def settings_from(bands):
    s = {"band_feather": 0.0}   # disable spatial feather for the core check
    for i, color in enumerate(COLORS):
        for p, param in enumerate(PARAMS):
            s[f"band_{color}_{param}"] = bands[i][p]
    return s


def main():
    src = sys.argv[1]
    sys.path.insert(0, src)
    from core import ccr_processor as cp

    H, W = 24, 32
    rs = np.random.RandomState(11)
    img = rs.randint(0, 65536, (H, W, 3)).astype(np.uint16)
    il = [int(v) for v in img.reshape(-1)]

    def z():
        return [[0.0, 0.0, 0.0, 0.0] for _ in range(7)]

    cases = []

    def add(name, bands):
        s = settings_from(bands)
        out = cp.apply_color_band_adjustments(img, s)
        cases.append(dict(name=name, w=W, h=H, bands=bands, feather=0.0,
                          inp=il, out=[int(v) for v in out.reshape(-1)]))

    b = z(); b[0][1] = 50.0                    # red sat +50
    add("red-sat", b)
    b = z(); b[5][2] = -40.0                   # blue bright -40
    add("blue-bright", b)
    b = z(); b[3][3] = 40.0                    # green hue +40
    add("green-hue", b)
    b = z(); b[1][0] = 60.0                    # skin subsat +60
    add("skin-subsat", b)
    b = z()                                    # combined
    b[0][1] = 40.0; b[3][3] = -30.0; b[5][0] = 50.0; b[2][2] = 25.0
    add("combined", b)
    b = z()                                    # all params on one band
    b[4] = [30.0, 40.0, -20.0, 25.0]           # cyan
    add("cyan-all", b)

    dest = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                        "..", "internal", "adjust", "testdata", "golden_bands.json")
    with open(dest, "w") as f:
        json.dump(cases, f)
    print(f"wrote {len(cases)} band cases → {os.path.normpath(dest)}")


if __name__ == "__main__":
    main()
