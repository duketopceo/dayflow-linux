// ThemeMath.js — pure-JS color math for the token layer (KTD4).
// .pragma library: stateless, importable by relative path, testable under
// qmltestrunner with no shell. All functions take/return "#rrggbb" hex
// strings or plain numbers; OKLCH conversion follows Björn Ottosson's
// reference implementation.
.pragma library

function hexToRgb(hex) {
    var h = String(hex || "").replace("#", "")
    if (h.length === 3) h = h[0]+h[0]+h[1]+h[1]+h[2]+h[2]
    var n = parseInt(h, 16)
    if (isNaN(n) || h.length !== 6) return null
    return [((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255]
}

function rgbToHex(r, g, b) {
    var c = function(v) { v = Math.max(0, Math.min(1, v)); return ("0" + Math.round(v * 255).toString(16)).slice(-2) }
    return "#" + c(r) + c(g) + c(b)
}

// sRGB channel -> linear
function _lin(c) { return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4) }

// Relative luminance (WCAG 2)
function luminance(hex) {
    var c = hexToRgb(hex)
    if (!c) return 0
    return 0.2126 * _lin(c[0]) + 0.7152 * _lin(c[1]) + 0.0722 * _lin(c[2])
}

// WCAG contrast ratio between two opaque colors.
function contrast(a, b) {
    var la = luminance(a), lb = luminance(b)
    var hi = Math.max(la, lb), lo = Math.min(la, lb)
    return (hi + 0.05) / (lo + 0.05)
}

// Composite a fg at alpha over an opaque bg, then contrast vs a third
// color — measures the color as it will actually render.
function composite(fgHex, bgHex, alpha) {
    var f = hexToRgb(fgHex), b = hexToRgb(bgHex)
    if (!f || !b) return null
    return rgbToHex(f[0] * alpha + b[0] * (1 - alpha),
                    f[1] * alpha + b[1] * (1 - alpha),
                    f[2] * alpha + b[2] * (1 - alpha))
}

// sRGB -> OKLCH { l, c, h } (l 0..1, c chroma, h degrees)
function toOklch(hex) {
    var c = hexToRgb(hex)
    if (!c) return null
    var r = _lin(c[0]), g = _lin(c[1]), b = _lin(c[2])
    var l = 0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b
    var m = 0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b
    var s = 0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b
    l = Math.cbrt(l); m = Math.cbrt(m); s = Math.cbrt(s)
    var L = 0.2104542553 * l + 0.7936177850 * m - 0.0040720468 * s
    var a = 1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s
    var bb = 0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s
    var H = Math.atan2(bb, a) * 180 / Math.PI
    if (H < 0) H += 360
    return { l: L, c: Math.sqrt(a * a + bb * bb), h: H }
}

function fromOklch(L, C, H) {
    var hr = H * Math.PI / 180
    var a = C * Math.cos(hr), bb = C * Math.sin(hr)
    var l3 = Math.pow(L + 0.3963377774 * a + 0.2158037573 * bb, 3)
    var m3 = Math.pow(L - 0.1055613458 * a - 0.0638541728 * bb, 3)
    var s3 = Math.pow(L - 0.0894841775 * a - 1.2914855480 * bb, 3)
    var r =  4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3
    var g = -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3
    var b = -0.0041960863 * l3 - 0.7034186147 * m3 + 1.7076147010 * s3
    var gam = function(c) { return c <= 0.0031308 ? 12.92 * c : 1.055 * Math.pow(c, 1 / 2.4) - 0.055 }
    return rgbToHex(gam(r), gam(g), gam(b))
}

// Contrast-correct `hue` against `bg` by moving lightness toward `fg`
// in fixed OKLCH steps until `target` is reached. `measure` composes the
// corrected hue the way the surface renders it (alpha over bg) before
// checking. If the step budget is exhausted, returns a straight fg-blend
// — never throws, always returns a valid hex.
function correctContrast(hue, bg, fg, target, alpha) {
    var a = alpha === undefined ? 1.0 : alpha
    var cur = hexToRgb(hue) ? hue : fg
    var check = function(c) {
        var comp = composite(c, bg, a)
        return comp ? contrast(comp, bg) : 0
    }
    if (check(cur) >= target) return cur
    var okl = toOklch(cur), oklFg = toOklch(fg) || { l: 0.9, c: 0, h: 0 }
    if (!okl) return fg
    var dir = oklFg.l > okl.l ? 1 : -1
    var steps = 40, step = 0.012 * dir
    for (var i = 0; i < steps; i++) {
        okl = { l: Math.max(0, Math.min(1, okl.l + step)), c: okl.c, h: okl.h }
        var cand = fromOklch(okl.l, okl.c, okl.h)
        if (check(cand) >= target) return cand
    }
    // Budget exhausted — blend toward fg directly.
    var f = hexToRgb(fg), h2 = hexToRgb(cur)
    if (!f || !h2) return fg
    return rgbToHex(h2[0] * 0.35 + f[0] * 0.65, h2[1] * 0.35 + f[1] * 0.65, h2[2] * 0.35 + f[2] * 0.65)
}
