#!/usr/bin/env python3
# FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
# Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.
"""A/B harness: run a battery of conversion+adjustment cases through BOTH the Go
port and the real FreeCCR Python pipeline on identical input pixels, then diff
the 16-bit outputs. Prints a table of max/mean abs diff per case.

  python ab.py <in.tif> --src <FreeCCR/src> --go <bin/freeccr> \
      --tifdiff <bin/tifdiff> --black R,G,B --white R,G,B [--ref x0,y0,x1,y1]
"""
import argparse
import os
import re
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))


def run(cmd, env=None):
    r = subprocess.run(cmd, capture_output=True, text=True, env=env)
    if r.returncode != 0:
        raise RuntimeError(f"cmd failed: {' '.join(cmd)}\n{r.stderr}\n{r.stdout}")
    return r.stdout


def diff(tifdiff, a, b):
    out = run([tifdiff, a, b])
    m = re.search(r"maxAbsDiff=(\d+)\s+meanAbsDiff=([\d.]+)\s+differing=([\d.]+)%", out)
    if not m:
        raise RuntimeError("unparseable tifdiff: " + out)
    return int(m.group(1)), float(m.group(2)), float(m.group(3))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("inp")
    ap.add_argument("--src", required=True)
    ap.add_argument("--go", required=True)
    ap.add_argument("--tifdiff", required=True)
    ap.add_argument("--black", required=True)
    ap.add_argument("--white", required=True)
    ap.add_argument("--ref", default="0.02,0.02,0.18,0.25")
    a = ap.parse_args()

    combo = ["--contrast", "35", "--saturation", "40", "--subsat", "25",
             "--exposure", "40", "--brightness", "15", "--highlights", "30",
             "--shadows", "25", "--temp", "30", "--tint", "-20",
             "--blackpoint", "20", "--whitepoint", "-15"]

    # (name, go-args, py-args). WS on = Go default / py --ws 1; WS off adds
    # --no-ws (Go) and --ws 0 (py).
    B, W = ["--black", a.black], ["--white", a.white]
    cases = [
        ("two-point density, WS on, no adj", B + W + ["--density"], ["--mode", "bwpoint"] + B + W + ["--density", "--ws", "1"]),
        ("two-point density, WS on, combo", B + W + ["--density"] + combo, ["--mode", "bwpoint"] + B + W + ["--density", "--ws", "1"] + combo),
        ("two-point linear,  WS on, no adj", B + W, ["--mode", "bwpoint"] + B + W + ["--ws", "1"]),
        ("two-point density, WS off, no adj", B + W + ["--density", "--no-ws"], ["--mode", "bwpoint"] + B + W + ["--density", "--ws", "0"]),
        ("two-point density, WS off, combo", B + W + ["--density", "--no-ws"] + combo, ["--mode", "bwpoint"] + B + W + ["--density", "--ws", "0"] + combo),
        ("default-slope,     WS on, no adj", B, ["--mode", "bwpoint"] + B + ["--ws", "1"]),
        ("default-slope,     WS on, combo", B + combo, ["--mode", "bwpoint"] + B + ["--ws", "1"] + combo),
        ("reference/auto,    no adj", ["--mode", "reference", "--ref", a.ref], ["--mode", "reference", "--ref", a.ref, "--ws", "1"]),
        ("reference/auto,    combo", ["--mode", "reference", "--ref", a.ref] + combo, ["--mode", "reference", "--ref", a.ref, "--ws", "1"] + combo),
    ]

    tmp = tempfile.mkdtemp(prefix="ab_")
    print(f"input: {a.inp}")
    print(f"{'case':38s} {'maxLSB':>7} {'meanLSB':>8} {'differ%':>8}  verdict")
    print("-" * 78)
    worst = 0
    for name, goargs, pyargs in cases:
        go_out = os.path.join(tmp, "go.tif")
        py_out = os.path.join(tmp, "py.tif")
        run([a.go, "convert", a.inp, "-o", go_out] + goargs)
        run([sys.executable, os.path.join(HERE, "ab_run.py"), a.inp, py_out,
             "--freeccr-src", a.src] + pyargs)
        mx, mean, dp = diff(a.tifdiff, go_out, py_out)
        worst = max(worst, mx)
        verdict = "exact" if mx == 0 else ("≤1 LSB" if mx <= 1 else ("≤2 LSB" if mx <= 2 else "DIFF"))
        print(f"{name:38s} {mx:7d} {mean:8.4f} {dp:8.3f}  {verdict}")
    print("-" * 78)
    print(f"worst-case max abs diff across all cases: {worst} LSB "
          f"({'PASS — parity' if worst <= 2 else 'FAIL'})")


if __name__ == "__main__":
    main()
