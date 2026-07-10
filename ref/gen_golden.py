#!/usr/bin/env python3
# FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
# Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.
"""Golden-fixture generator for the FreeCCR-go port.

Transliterates the numeric kernels from FreeCCR's src/core/ccr_processor.py
(convert + adjust) using pure numpy, and emits JSON fixtures that the Go tests
compare against. Only numpy is required — cv2 operations are reproduced with
their numpy equivalents (cv2.transform → weighted sum, cv2.pow → np.power,
cv2.exp → np.exp), all in float32 to track the reference pipeline.

Run:  python3 ref/gen_golden.py
Emits: internal/convert/testdata/golden.json
"""
import json
import os
import numpy as np

# --- constants (ccr_processor.py) -------------------------------------------
DEFAULT_DENSITY_SLOPE = 0.8
DEFAULT_DENSITY_GAMMA = 1.0
DENSITY_FLOOR = 1.0
WS_BITS = 10
WS_LO = 0.5
WS_WIDTH = float(1 << WS_BITS)
WS_B = WS_LO * WS_WIDTH
WS_W = (1.0 + WS_LO) * WS_WIDTH
LUM = np.array([[0.299, 0.587, 0.114]], dtype=np.float32)


def encode_window(d):
    code = d.astype(np.float32).copy()
    code *= np.float32(WS_W - WS_B)
    code += np.float32(WS_B)
    np.clip(code, 0.0, 65535.0, out=code)
    return code.astype(np.uint16)


def twopoint_invert(img_f, black, white, density, ws):
    img_f = img_f.astype(np.float32)
    if ws:
        d = np.empty_like(img_f)
        for c in range(3):
            base = max(float(black[c]), 1.0)
            dense = max(float(white[c]), 1.0)
            if density:
                dmax = float(np.log10(base / dense)) if base > dense else 0.0
                if dmax <= 1e-6:
                    d[..., c] = 0.0
                    continue
                ch = np.maximum(img_f[..., c], DENSITY_FLOOR)
                ch = base / ch
                ch = np.log10(ch)
                ch = ch / dmax
                d[..., c] = ch
            else:
                denom = base - dense
                if abs(denom) < 1.0:
                    d[..., c] = 1.0
                    continue
                t = (img_f[..., c] - dense) / denom
                d[..., c] = 1.0 - t
        return encode_window(d)

    norm = np.empty_like(img_f)
    for c in range(3):
        base = max(float(black[c]), 1.0)
        dense = max(float(white[c]), 1.0)
        if density:
            dmax = float(np.log10(base / dense)) if base > dense else 0.0
            if dmax <= 1e-6:
                norm[..., c] = 0.0
                continue
            ch = np.maximum(img_f[..., c], DENSITY_FLOOR)
            ch = base / ch
            ch = np.log10(ch)
            ch = ch / dmax
            ch = np.clip(ch, 0.0, 1.0)
            norm[..., c] = ch * 65535.0
        else:
            denom = base - dense
            if abs(denom) < 1.0:
                norm[..., c] = 0.0
                continue
            v = (img_f[..., c] - dense) / denom * 65535.0
            norm[..., c] = np.clip(v, 0, 65535)
    if density:
        return np.clip(norm, 0, 65535).astype(np.uint16)
    return (65535.0 - norm).clip(0, 65535).astype(np.uint16)


def default_slope_invert(img_f, black, slopes, ws):
    img_f = img_f.astype(np.float32)
    out = np.empty_like(img_f)
    for c in range(3):
        base = max(float(black[c]), 1.0)
        slope = DEFAULT_DENSITY_SLOPE if slopes is None else float(slopes[c])
        ch = np.maximum(img_f[..., c], DENSITY_FLOOR)
        ch = base / ch
        ch = np.log10(ch)
        ch = np.maximum(ch, 0.0)
        ch = ch * np.float32(slope)
        out[..., c] = ch
    if ws:
        if DEFAULT_DENSITY_GAMMA != 1.0:
            out = np.power(out, np.float32(1.0 / DEFAULT_DENSITY_GAMMA))
        return encode_window(out)
    np.clip(out, 0.0, 1.0, out=out)
    if DEFAULT_DENSITY_GAMMA != 1.0:
        out = np.power(out, np.float32(1.0 / DEFAULT_DENSITY_GAMMA))
    out = out * np.float32(65535.0)
    return out.astype(np.uint16)


def postinvert_look(rgb_inverted):
    x = rgb_inverted.astype(np.float32)
    x *= np.float32(1.0 / 65535.0)
    np.clip(x, 0.0, 1.0, out=x)
    luminance = (x * LUM).sum(axis=-1).astype(np.float32)
    sat_curve = np.power(luminance, np.float32(0.8)).astype(np.float32)
    dynamic = (np.float32(0.15) * sat_curve + np.float32(1.0)).astype(np.float32)
    lum3 = luminance[..., None]
    x = ((x - lum3) * dynamic[..., None] + lum3).astype(np.float32)
    np.clip(x, 0.0, 1.0, out=x)
    shadow_lum = (x * LUM).sum(axis=-1).astype(np.float32)
    warmth = (np.exp(shadow_lum * np.float32(-4.0)) * np.float32(0.35)).astype(np.float32)
    green = (np.exp(shadow_lum * np.float32(-3.5)) * np.float32(0.15)).astype(np.float32)
    x[..., 0] *= (np.float32(1.0) + warmth * np.float32(0.8))
    x[..., 1] *= (np.float32(1.0) + green)
    x[..., 2] *= (np.float32(1.0) - warmth)
    x *= np.float32(65535.0)
    np.clip(x, 0, 65535, out=x)
    return x.astype(np.uint16)


def compute_reference_norm_params(ref_crop):
    ref_crop = ref_crop.astype(np.float32)
    p_lo = np.empty(3, np.float64)
    p_hi = np.empty(3, np.float64)
    norm_crop = np.empty_like(ref_crop)
    for c in range(3):
        p_lo[c] = np.percentile(ref_crop[..., c], 1)
        p_hi[c] = np.percentile(ref_crop[..., c], 99)
        norm_crop[..., c] = np.clip(
            (ref_crop[..., c] - p_lo[c]) / (p_hi[c] - p_lo[c])
            * (65535 - 8192) + 8192, 0, 65535)
    od_crop = -np.log10((norm_crop + 1e-6) / 65535.0)
    mean_od = np.mean(od_crop, axis=(0, 1))
    target = np.mean(mean_od)
    od_factors = target / (mean_od + 1e-12)
    return p_lo, p_hi, od_factors


def apply_reference_normalization(img, p_lo, p_hi, od_factors):
    affine = np.zeros((3, 4), dtype=np.float64)
    for c in range(3):
        s = (65535.0 - 8192.0) / (p_hi[c] - p_lo[c])
        affine[c, c] = s / 65535.0
        affine[c, 3] = (8192.0 - p_lo[c] * s) / 65535.0
    x = img.astype(np.float32)
    norm = np.empty_like(x)
    for c in range(3):
        norm[..., c] = x[..., c] * np.float32(affine[c, c]) + np.float32(affine[c, 3])
    np.clip(norm, 0.0, 1.0, out=norm)
    norm += np.float32(1e-6 / 65535.0)
    for c in range(3):
        norm[..., c] = np.power(norm[..., c], np.float32(od_factors[c]))
    norm *= np.float32(65535.0)
    np.clip(norm, 0, 65535, out=norm)
    inverted = 65535 - norm.astype(np.uint16)
    return postinvert_look(inverted)


WB_TEMP_STRENGTH = 0.40
WB_TINT_STRENGTH = 0.26
WS_INV_WIDTH = 1.0 / (WS_W - WS_B)
WS_HEADROOM_STOPS = float(np.log2((65535.0 - WS_B) / (WS_W - WS_B)))


def white_balance_gains(kelvin, tint, balance=1.0):
    gr = gg = gb = 1.0
    if kelvin != 0.0:
        s = (kelvin / 100.0) * WB_TEMP_STRENGTH
        gr *= (1.0 + s)
        gb *= (1.0 - s)
    if tint != 0.0:
        t = float(np.tanh(tint * 0.02)) * WB_TINT_STRENGTH * balance
        gg *= (1.0 - t)
        gr *= (1.0 + 0.3 * t)
        gb *= (1.0 + 0.3 * t)
    return float(gr), float(gg), float(gb)


def working_space_recovery(img16, exposure, white_point, kelvin, tint, balance):
    d = img16.astype(np.float32)
    d -= np.float32(WS_B)
    d *= np.float32(WS_INV_WIDTH)
    if kelvin != 0.0 or tint != 0.0:
        gr, gg, gb = white_balance_gains(kelvin, tint, balance)
        if gr != 1.0:
            d[..., 0] *= np.float32(gr)
        if gg != 1.0:
            d[..., 1] *= np.float32(gg)
        if gb != 1.0:
            d[..., 2] *= np.float32(gb)
    if white_point != 0.0:
        wp = float(np.clip(white_point, -100.0, 100.0))
        d *= np.float32(2.0 ** (WS_HEADROOM_STOPS * wp / 100.0))
    if exposure != 0.0:
        white_val = 1.0 - np.clip(exposure, -200.0, 200.0) / 300.0
        d *= np.float32(1.0 / white_val)
    np.clip(d, 0.0, 1.0, out=d)
    d *= np.float32(65535.0)
    return d.astype(np.uint16)


def adjust_image(img16, ws_windowed=False, **kw):
    """Faithful transliteration of adjust_image (sliders only, no bands)."""
    g = lambda k: float(kw.get(k, 0.0))
    kelvin_shift = g("kelvin_shift"); tint_shift = g("tint_shift")
    exposure = g("exposure"); brightness = g("brightness")
    blackpoint = g("blackpoint"); whitepoint = g("whitepoint")
    contrast = g("contrast"); saturation = g("saturation")
    tint_balance_factor = float(kw.get("tint_balance_factor", 1.0))
    highlights = g("highlights"); shadows = g("shadows")
    sub_saturation = g("sub_saturation")
    ch_input_gain = g("ch_input_gain")
    ch_master_shift = g("ch_master_shift"); ch_master_gain = g("ch_master_gain")
    ch_r_shift = g("ch_r_shift"); ch_r_gain = g("ch_r_gain"); ch_r_blackpoint = g("ch_r_blackpoint")
    ch_g_shift = g("ch_g_shift"); ch_g_gain = g("ch_g_gain"); ch_g_blackpoint = g("ch_g_blackpoint")
    ch_b_shift = g("ch_b_shift"); ch_b_gain = g("ch_b_gain"); ch_b_blackpoint = g("ch_b_blackpoint")

    if ws_windowed:
        img16 = working_space_recovery(img16, exposure, whitepoint,
                                       kelvin_shift, tint_shift, tint_balance_factor)
        exposure = 0.0; whitepoint = 0.0; kelvin_shift = 0.0; tint_shift = 0.0
    img = img16.astype(np.float32)

    if kelvin_shift != 0.0 or tint_shift != 0.0:
        _gr, _gg, _gb = white_balance_gains(kelvin_shift, tint_shift, tint_balance_factor)
        img[..., 0] *= np.float32(_gr)
        img[..., 1] *= np.float32(_gg)
        img[..., 2] *= np.float32(_gb)

    saturation_scale = 1.0 + (saturation / 100.0)

    if exposure != 0.0:
        gm = np.clip(exposure, -200.0, 200.0) / 300.0
        white_val = 1.0 - gm
        img = np.clip(img / 65535.0 / white_val, 0.0, 1.0) * 65535.0
    if brightness != 0.0:
        img_norm = img / 65535.0
        curve = 1.0 - 0.3 * (brightness / 8.0)
        img_norm = np.power(img_norm, curve)
        img_norm = np.clip(img_norm, 0.0, 1.0)
        img = img_norm * 65535.0
    if highlights != 0.0 or shadows != 0.0:
        HS_PEAK = 0.10546875
        HS_STRENGTH = 0.30
        x = img / 65535.0
        one_minus = 1.0 - x
        wh = (x ** 3) * one_minus / HS_PEAK
        ws = x * (one_minus ** 3) / HS_PEAK
        x = x + (highlights / 100.0) * HS_STRENGTH * wh + (shadows / 100.0) * HS_STRENGTH * ws
        img = np.clip(x, 0.0, 1.0) * 65535.0
    if blackpoint != 0.0 or whitepoint != 0.0:
        img_norm = img / 65535.0
        black_clip = np.clip(blackpoint, -100, 100) / 300.0
        white_clip = np.clip(whitepoint, -100, 100) / 300.0
        black_val = 0.0 + black_clip
        white_val = 1.0 - white_clip
        img_norm = (img_norm - black_val) / (white_val - black_val)
        img_norm = np.clip(img_norm, 0, 1)
        img = img_norm * 65535.0
    if contrast != 0.0:
        img_norm = img / 65535.0
        midpoint = 0.5
        k = np.clip(contrast / 105.0, -0.95, 0.95)
        img_norm = ((1 + k) * (img_norm - midpoint)) / (1 + k * np.abs(img_norm - midpoint) * 2) + midpoint
        img = img_norm * 65535.0
    if saturation != 0.0:
        img_norm = img / 65535.0
        gray = np.dot(img_norm[..., :3], [0.299, 0.587, 0.114])
        gray_expanded = np.expand_dims(gray, axis=-1)
        mid_high_weight = np.exp(-((gray - 0.50) / 0.35) ** 2)
        min_saturation_factor = 0.2
        saturation_curve = min_saturation_factor + (1.0 - min_saturation_factor) * mid_high_weight
        dynamic_saturation_scale = 1.0 + (saturation_scale - 1.0) * saturation_curve
        dynamic_saturation_scale = np.expand_dims(dynamic_saturation_scale, axis=-1)
        img_norm = gray_expanded + dynamic_saturation_scale * (img_norm - gray_expanded)
        img_norm = np.clip(img_norm, 0, 1)
        img = img_norm * 65535.0
    if sub_saturation != 0.0:
        img_norm = np.clip(img / 65535.0, 0.0, 1.0)
        mx = np.max(img_norm, axis=-1, keepdims=True)
        gamma_s = 2.0 ** (sub_saturation / 100.0)
        safe_mx = np.maximum(mx, 1e-6)
        img_norm = np.where(mx > 1e-6, mx * (img_norm / safe_mx) ** gamma_s, img_norm)
        img = img_norm * 65535.0
    _ch_active = (ch_input_gain, ch_master_shift, ch_master_gain,
                  ch_r_shift, ch_r_gain, ch_r_blackpoint,
                  ch_g_shift, ch_g_gain, ch_g_blackpoint,
                  ch_b_shift, ch_b_gain, ch_b_blackpoint)
    if any(p != 0.0 for p in _ch_active):
        ig = 2.0 ** (ch_input_gain / 50.0)
        shifts = (np.clip(ch_master_shift + ch_r_shift, -100, 100) / 300.0,
                  np.clip(ch_master_shift + ch_g_shift, -100, 100) / 300.0,
                  np.clip(ch_master_shift + ch_b_shift, -100, 100) / 300.0)
        gains = (np.clip(ch_master_gain + ch_r_gain, -100, 100) / 300.0,
                 np.clip(ch_master_gain + ch_g_gain, -100, 100) / 300.0,
                 np.clip(ch_master_gain + ch_b_gain, -100, 100) / 300.0)
        blacks = (np.clip(ch_r_blackpoint, -100, 100) / 300.0,
                  np.clip(ch_g_blackpoint, -100, 100) / 300.0,
                  np.clip(ch_b_blackpoint, -100, 100) / 300.0)
        for cc in range(3):
            if ig == 1.0 and shifts[cc] == 0.0 and gains[cc] == 0.0 and blacks[cc] == 0.0:
                continue
            ch = img[..., cc] / 65535.0
            if ig != 1.0:
                ch = ch * ig
            if shifts[cc] != 0.0:
                ch = ch + shifts[cc]
            black_val = blacks[cc]
            white_val = 1.0 - gains[cc]
            if black_val != 0.0 or white_val != 1.0:
                ch = (ch - black_val) / (white_val - black_val)
            img[..., cc] = np.clip(ch, 0.0, 1.0) * 65535.0

    img = np.clip(img, 0, 65535)
    return img.astype(np.uint16)


# --- Curves / Gamma / Cineon (pure numpy, transliterated) -------------------
_IDENTITY_POINTS = [[0.0, 0.0], [255.0, 255.0]]
_CURVE_CHANNELS = ("rgb", "r", "g", "b")


def _normalize_points(points):
    if not points:
        return None
    cleaned = []
    for p in points:
        x = float(p[0]); y = float(p[1])
        if not (np.isfinite(x) and np.isfinite(y)):
            continue
        cleaned.append([min(255.0, max(0.0, x)), min(255.0, max(0.0, y))])
    if len(cleaned) < 2:
        return None
    cleaned.sort(key=lambda q: q[0])
    dedup = [cleaned[0]]
    for q in cleaned[1:]:
        if q[0] > dedup[-1][0]:
            dedup.append(q)
    if len(dedup) < 2:
        return None
    if dedup == _IDENTITY_POINTS:
        return None
    return dedup


def _monotone_cubic(xs, ys, xq):
    xs = np.asarray(xs, dtype=np.float64); ys = np.asarray(ys, dtype=np.float64)
    n = len(xs)
    if n == 2:
        return np.interp(xq, xs, ys)
    h = np.diff(xs); delta = np.diff(ys) / h
    m = np.empty(n, dtype=np.float64)
    m[1:-1] = (delta[:-1] + delta[1:]) / 2.0
    m[0] = delta[0]; m[-1] = delta[-1]
    for i in range(n - 1):
        if delta[i] == 0.0:
            m[i] = 0.0; m[i + 1] = 0.0
        else:
            a = m[i] / delta[i]; b = m[i + 1] / delta[i]; s = a * a + b * b
            if s > 9.0:
                t = 3.0 / np.sqrt(s)
                m[i] = t * a * delta[i]; m[i + 1] = t * b * delta[i]
    xq = np.asarray(xq, dtype=np.float64)
    idx = np.clip(np.searchsorted(xs, xq) - 1, 0, n - 2)
    x0 = xs[idx]; x1 = xs[idx + 1]; y0 = ys[idx]; y1 = ys[idx + 1]
    m0 = m[idx]; m1 = m[idx + 1]
    hh = (x1 - x0); t = (xq - x0) / hh; t2 = t * t; t3 = t2 * t
    h00 = 2 * t3 - 3 * t2 + 1; h10 = t3 - 2 * t2 + t
    h01 = -2 * t3 + 3 * t2; h11 = t3 - t2
    return h00 * y0 + h10 * hh * m0 + h01 * y1 + h11 * hh * m1


def build_channel_lut(points):
    pts = _normalize_points(points)
    x = np.arange(256, dtype=np.float64)
    if pts is None:
        return x.astype(np.float32)
    xs = [p[0] for p in pts]; ys = [p[1] for p in pts]
    return np.clip(_monotone_cubic(xs, ys, x), 0.0, 255.0).astype(np.float32)


_LUT16_SAMPLE = np.arange(65536, dtype=np.float32)
_LUT16_SRC_IDX = np.arange(256, dtype=np.float32) * (65535.0 / 255.0)


def _expand_curve256_to_lut16(curve256):
    return np.interp(_LUT16_SAMPLE, _LUT16_SRC_IDX,
                     curve256 * (65535.0 / 255.0)).astype(np.uint16)


def _is_identity_curves(curves):
    if not curves:
        return True
    for ch in _CURVE_CHANNELS:
        if _normalize_points(curves.get(ch)) is not None:
            return False
    return True


def apply_curves(img16, curves):
    if _is_identity_curves(curves):
        return img16
    rgb_lut = build_channel_lut(curves.get("rgb"))
    rgb_idx = np.clip(np.rint(rgb_lut), 0, 255).astype(np.intp)
    out = np.empty_like(img16)
    for c, key in enumerate(("r", "g", "b")):
        ch_lut = build_channel_lut(curves.get(key))
        lut16 = _expand_curve256_to_lut16(ch_lut[rgb_idx])
        out[..., c] = lut16[img16[..., c]]
    return out


_GAMMA_MAX_OFFSET = 63.75


def gamma_curve_points(gamma):
    offset = (float(gamma) / 100.0) * _GAMMA_MAX_OFFSET
    return [[0.0, 0.0], [127.5 - offset, 127.5 + offset], [255.0, 255.0]]


def _gamma_lut16(gamma):
    curve256 = np.rint(build_channel_lut(gamma_curve_points(gamma)))
    return _expand_curve256_to_lut16(curve256)


_GAMMA_LUMA = np.array([0.299, 0.587, 0.114], dtype=np.float32)


def _apply_gamma_luminance(img16, gamma):
    lut16 = _gamma_lut16(gamma)
    rgbf = img16.astype(np.float32)
    lum = rgbf @ _GAMMA_LUMA
    idx = np.clip(np.rint(lum), 0, 65535).astype(np.uint16)
    lum_out = lut16[idx].astype(np.float32)
    k = lum_out / np.maximum(lum, 1.0)
    out = rgbf * k[..., np.newaxis]
    return np.clip(np.rint(out), 0.0, 65535.0).astype(np.uint16)


def apply_gamma_curve(img16, gamma, luminance=False):
    if not gamma:
        return img16
    if luminance:
        return _apply_gamma_luminance(img16, gamma)
    return apply_curves(img16, {"rgb": gamma_curve_points(gamma)})


def cineon_lut16():
    code = np.linspace(0.0, 1023.0, 65536, dtype=np.float64)
    gain = 0.002 / 0.6
    off = 10.0 ** ((95.0 - 685.0) * gain)
    lin = (10.0 ** ((code - 685.0) * gain) - off) / (1.0 - off)
    lin = np.clip(lin, 0.0, 1.0)
    return np.round(np.power(lin, 1.0 / 2.2) * 65535.0).astype(np.uint16)


def apply_cineon(img16):
    return cineon_lut16()[img16]


def rnd(shape, seed, lo=1.0, hi=65535.0):
    rs = np.random.RandomState(seed)
    return (rs.uniform(lo, hi, shape)).astype(np.float32)


def img_list(a):
    return [float(v) for v in a.reshape(-1)]


def out_list(a):
    return [int(v) for v in a.reshape(-1)]


def main():
    cases = []
    H, W = 6, 7  # small, includes non-multiple sizes

    # --- two-point cases -------------------------------------------------
    black = [60000.0, 61000.0, 59000.0]
    white = [3000.0, 2500.0, 3500.0]
    for density in (False, True):
        for ws in (False, True):
            img = rnd((H, W, 3), 100 + int(density) * 2 + int(ws))
            out = twopoint_invert(img, black, white, density, ws)
            cases.append(dict(kind="twopoint", density=density, ws=ws,
                              black=black, white=white, w=W, h=H,
                              inp=img_list(img), out=out_list(out)))
    # degenerate channel (base<=dense) both modes
    dblack = [60000.0, 2000.0, 59000.0]
    dwhite = [3000.0, 2500.0, 3500.0]  # ch1 base<dense
    for density in (False, True):
        for ws in (False, True):
            img = rnd((H, W, 3), 200 + int(density) * 2 + int(ws))
            out = twopoint_invert(img, dblack, dwhite, density, ws)
            cases.append(dict(kind="twopoint", density=density, ws=ws,
                              black=dblack, white=dwhite, w=W, h=H,
                              inp=img_list(img), out=out_list(out)))

    # --- default-slope cases --------------------------------------------
    for ws in (False, True):
        img = rnd((H, W, 3), 300 + int(ws))
        out = default_slope_invert(img, black, None, ws)
        cases.append(dict(kind="defslope", ws=ws, black=black, slopes=None,
                          w=W, h=H, inp=img_list(img), out=out_list(out)))
    slopes = [0.7, 0.85, 0.9]
    for ws in (False, True):
        img = rnd((H, W, 3), 320 + int(ws))
        out = default_slope_invert(img, black, slopes, ws)
        cases.append(dict(kind="defslope", ws=ws, black=black, slopes=slopes,
                          w=W, h=H, inp=img_list(img), out=out_list(out)))

    # --- post-invert look (standalone) ----------------------------------
    for seed in (400, 401):
        img = rnd((H, W, 3), seed, 0.0, 65535.0)
        imgu = img.astype(np.uint16)
        out = postinvert_look(imgu)
        cases.append(dict(kind="look", w=W, h=H,
                          inp=[int(v) for v in imgu.reshape(-1)],
                          out=out_list(out)))

    # --- reference params + apply ---------------------------------------
    crop = rnd((10, 10, 3), 500)
    p_lo, p_hi, odf = compute_reference_norm_params(crop)
    img = rnd((H, W, 3), 501)
    out = apply_reference_normalization(img, p_lo, p_hi, odf)
    cases.append(dict(kind="refparams",
                      crop_w=10, crop_h=10, crop=img_list(crop),
                      p_lo=[float(v) for v in p_lo],
                      p_hi=[float(v) for v in p_hi],
                      od_factors=[float(v) for v in odf]))
    cases.append(dict(kind="refapply", w=W, h=H, inp=img_list(img),
                      p_lo=[float(v) for v in p_lo],
                      p_hi=[float(v) for v in p_hi],
                      od_factors=[float(v) for v in odf],
                      out=out_list(out)))
    # End-to-end: crop → params → apply. Go recomputes params itself; this is
    # the meaningful fidelity test (float32 percentile rounding in the raw
    # params is invisible in the uint16 output).
    cases.append(dict(kind="reffull", w=W, h=H, crop_w=10, crop_h=10,
                      crop=img_list(crop), inp=img_list(img), out=out_list(out)))

    here = os.path.dirname(os.path.abspath(__file__))
    dest_dir = os.path.join(here, "..", "internal", "convert", "testdata")
    os.makedirs(dest_dir, exist_ok=True)
    dest = os.path.join(dest_dir, "golden.json")
    with open(dest, "w") as f:
        json.dump(cases, f)
    print(f"wrote {len(cases)} cases → {os.path.normpath(dest)}")

    # --- adjust_image cases ---------------------------------------------
    adj = []
    # Single-slider sweeps covering both signs.
    singles = [
        ("kelvin_shift", 60.0), ("kelvin_shift", -60.0),
        ("tint_shift", 50.0), ("tint_shift", -50.0),
        ("exposure", 120.0), ("exposure", -150.0),
        ("brightness", 40.0), ("brightness", -40.0),
        ("blackpoint", 50.0), ("whitepoint", -50.0),
        ("contrast", 70.0), ("contrast", -70.0),
        ("saturation", 60.0), ("saturation", -80.0),
        ("highlights", 80.0), ("shadows", 80.0),
        ("sub_saturation", 60.0), ("sub_saturation", -60.0),
        ("ch_input_gain", 40.0), ("ch_master_shift", 30.0),
        ("ch_master_gain", 30.0), ("ch_r_gain", 40.0),
        ("ch_g_shift", -25.0), ("ch_b_blackpoint", 35.0),
    ]
    sc = 0
    for key, val in singles:
        img = rnd((H, W, 3), 700 + sc).astype(np.uint16)
        out = adjust_image(img, **{key: val})
        adj.append(dict(w=W, h=H, ws=False, params={key: val},
                        inp=[int(v) for v in img.reshape(-1)], out=out_list(out)))
        sc += 1
    # Combined full-chain settings.
    combo = dict(kelvin_shift=30.0, tint_shift=-20.0, exposure=40.0,
                 brightness=15.0, highlights=30.0, shadows=25.0,
                 blackpoint=20.0, whitepoint=-15.0, contrast=35.0,
                 saturation=40.0, sub_saturation=25.0,
                 ch_input_gain=10.0, ch_master_gain=8.0, ch_r_gain=12.0,
                 ch_g_shift=-6.0, ch_b_blackpoint=9.0)
    img = rnd((H, W, 3), 800).astype(np.uint16)
    out = adjust_image(img, **combo)
    adj.append(dict(w=W, h=H, ws=False, params=combo,
                    inp=[int(v) for v in img.reshape(-1)], out=out_list(out)))
    # Working-space windowed recovery (values inside the window ~[512,1536]).
    for seed, params in [(810, dict(exposure=60.0)),
                         (811, dict(whitepoint=-40.0)),
                         (812, dict(kelvin_shift=40.0, tint_shift=20.0)),
                         (813, dict(exposure=-30.0, whitepoint=-50.0, saturation=30.0))]:
        rs = np.random.RandomState(seed)
        img = rs.uniform(400.0, 1700.0, (H, W, 3)).astype(np.uint16)
        out = adjust_image(img, ws_windowed=True, **params)
        adj.append(dict(w=W, h=H, ws=True, params=params,
                        inp=[int(v) for v in img.reshape(-1)], out=out_list(out)))

    adest_dir = os.path.join(here, "..", "internal", "adjust", "testdata")
    os.makedirs(adest_dir, exist_ok=True)
    adest = os.path.join(adest_dir, "golden_adjust.json")
    with open(adest, "w") as f:
        json.dump(adj, f)
    print(f"wrote {len(adj)} adjust cases → {os.path.normpath(adest)}")

    # --- curves / gamma / cineon cases ----------------------------------
    cv = []
    img = rnd((H, W, 3), 900).astype(np.uint16)
    il = [int(v) for v in img.reshape(-1)]

    def curve_case(kind, curves=None, gamma=0.0, luminance=False):
        if kind == "curves":
            out = apply_curves(img, curves)
        elif kind == "gamma":
            out = apply_gamma_curve(img, gamma, luminance)
        else:
            out = apply_cineon(img)
        cv.append(dict(kind=kind, w=W, h=H, inp=il,
                       curves=curves, gamma=gamma, luminance=luminance,
                       out=out_list(out)))

    # S-curve on the composite, plus a per-channel move.
    curve_case("curves", {"rgb": [[0, 0], [64, 48], [192, 208], [255, 255]]})
    curve_case("curves", {"r": [[0, 20], [255, 235]], "b": [[0, 0], [128, 100], [255, 255]]})
    curve_case("curves", {"rgb": [[0, 0], [128, 160], [255, 255]],
                          "g": [[0, 0], [96, 80], [255, 255]]})
    # 5-point wiggle to exercise Fritsch-Carlson monotonicity clamp.
    curve_case("curves", {"rgb": [[0, 0], [50, 90], [120, 110], [200, 230], [255, 255]]})
    curve_case("gamma", gamma=50.0)
    curve_case("gamma", gamma=-50.0)
    curve_case("gamma", gamma=40.0, luminance=True)
    curve_case("gamma", gamma=-60.0, luminance=True)
    curve_case("cineon")

    cvdest = os.path.join(adest_dir, "golden_curves.json")
    with open(cvdest, "w") as f:
        json.dump(cv, f)
    print(f"wrote {len(cv)} curve cases → {os.path.normpath(cvdest)}")


if __name__ == "__main__":
    main()
