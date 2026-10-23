// tst_thememath.qml — AE1 scenarios for ThemeMath.js (pure JS, no shell).
// Run: qmltestrunner -input tests/qml/tst_thememath.qml
import QtQuick
import QtTest
import "../../ThemeMath.js" as TM

TestCase {
    name: "ThemeMath"

    function init() {}

    // flexoki-light background + its yellow: corrected fill at 0.85 alpha
    // over bg must reach 3:1.
    function test_flexokiYellowCorrectsToFillTarget() {
        var bg = "#FFFCF0", fg = "#100F0F"
        var fixed = TM.correctContrast("#D0A215", bg, fg, 3.0, 0.85)
        var comp = TM.composite(fixed, bg, 0.85)
        verify(TM.contrast(comp, bg) >= 3.0,
               "corrected flexoki yellow fill contrast " + TM.contrast(comp, bg))
    }

    // flexoki blue already passes — must come back unchanged.
    function test_passingHueUnchanged() {
        verify(TM.correctContrast("#205EA6", "#FFFCF0", "#100F0F", 3.0, 0.85) === "#205ea6"
               || TM.correctContrast("#205EA6", "#FFFCF0", "#100F0F", 3.0, 0.85) === "#205EA6")
    }

    // 4.5:1 text correction on a dark theme lightens a dark hue until it
    // passes; hue angle stays within tolerance.
    function test_darkThemeLightensHueTowardFg() {
        var bg = "#101315", fg = "#eceff1"
        var fixed = TM.correctContrast("#1a3a8a", bg, fg, 4.5, 1.0)
        verify(TM.contrast(fixed, bg) >= 4.5, "text contrast after fix " + TM.contrast(fixed, bg))
        var before = TM.toOklch("#1a3a8a"), after = TM.toOklch(fixed)
        var dh = Math.abs(after.h - before.h)
        if (dh > 180) dh = 360 - dh
        verify(dh < 12, "hue angle drift " + dh)
    }

    // Near-bg hue with an unreachable 21:1 target exhausts the step budget
    // and falls through to the fg-blend path (≥4.5 on dark bg).
    function test_budgetExhaustionBlendsTowardFg() {
        var bg = "#101315", fg = "#eceff1"
        var out = TM.correctContrast("#101315", bg, fg, 21.0, 1.0)
        verify(/^#[0-9a-f]{6}$/i.test(out), "output is a hex: " + out)
        verify(TM.contrast(out, bg) >= 4.5,
               "fg-blend fallback contrast " + TM.contrast(out, bg))
    }

    // composite() sanity: alpha 1 = the hue itself.
    function test_compositeOpaque() {
        verify(TM.composite("#ff0000", "#000000", 1.0) === "#ff0000")
    }
}
