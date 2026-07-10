#!/usr/bin/env python3
# FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
# Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.
"""A/B driver: run the REAL FreeCCR Python pipeline (src/core/ccr_processor.py)
on an input scan and write a 16-bit RGB TIFF, so its output can be compared
against the Go port on identical pixels.

Uses the actual cv2-based kernels (apply_bwpoint_normalization / adjust_image /
compute_reference_norm_params / apply_reference_normalization), not the numpy
transliteration in gen_golden.py — this is an independent check of the port.

  python ab_run.py <in.tif> <out.tif> --mode bwpoint \
      --black R,G,B [--white R,G,B] [--density] [--ws 0|1] \
      [--exposure N --contrast N ...]
"""
import argparse
import os
import sys

import numpy as np
import tifffile


def triple(s):
    return [float(x) for x in s.split(",")]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("inp")
    ap.add_argument("out")
    ap.add_argument("--freeccr-src", required=True, help="path to FreeCCR/src")
    ap.add_argument("--mode", default="bwpoint")
    ap.add_argument("--black", default="")
    ap.add_argument("--white", default="")
    ap.add_argument("--ref", default="", help="x0,y0,x1,y1 normalized")
    ap.add_argument("--density", action="store_true")
    ap.add_argument("--ws", default="1")
    for k in ("exposure", "brightness", "contrast", "saturation", "temp", "tint",
              "highlights", "shadows", "blackpoint", "whitepoint", "subsat"):
        ap.add_argument("--" + k, type=float, default=0.0)
    a = ap.parse_args()

    # Working-space toggle must be set before importing ccr_processor (the window
    # geometry constants are read at import time).
    os.environ["FREECCR_WORKING_SPACE"] = "1" if a.ws == "1" else "0"
    sys.path.insert(0, a.freeccr_src)
    from core import ccr_processor as cp

    ws = a.ws == "1"

    img = tifffile.imread(a.inp)
    img = np.ascontiguousarray(img[..., :3]).astype(np.uint16)  # RGB uint16

    adj = dict(kelvin_shift=a.temp, tint_shift=a.tint, exposure=a.exposure,
               brightness=a.brightness, blackpoint=a.blackpoint,
               whitepoint=a.whitepoint, contrast=a.contrast,
               saturation=a.saturation, highlights=a.highlights,
               shadows=a.shadows, sub_saturation=a.subsat)

    if a.mode == "reference":
        x0, y0, x1, y1 = triple(a.ref) + [0] * (4 - len(triple(a.ref)))
        h, w = img.shape[:2]
        rect = (int(x0 * w), int(y0 * h), int(x1 * w), int(y1 * h))  # pixel x1,y1,x2,y2
        p_lo, p_hi, odf = cp.compute_reference_norm_params(img, rect, 0)
        conv = cp.apply_reference_normalization(img, p_lo, p_hi, odf)
        final = cp.adjust_image(conv, ws_windowed=False, **adj)
    else:
        black = triple(a.black)
        white = triple(a.white) if a.white else None
        conv = cp.apply_bwpoint_normalization(img, black, white, density=a.density)
        final = cp.adjust_image(conv, ws_windowed=ws, **adj)

    tifffile.imwrite(a.out, final.astype(np.uint16), photometric="rgb")
    print(f"py: {a.out} {final.shape} ws={ws} mode={a.mode}")


if __name__ == "__main__":
    main()
