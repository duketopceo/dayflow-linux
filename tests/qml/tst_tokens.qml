// tst_tokens.qml — AE3: Tokens re-parses a staged colors.toml swap and
// recomputes roles. Runs under qmltestrunner with the qs/Commons stub.
// Run: qmltestrunner -input tests/qml/tst_tokens.qml -import tests/qml
import QtQuick
import QtTest
import "../.."

TestCase {
    name: "Tokens"
    property var tk: null

    function init() {
        var c = Qt.createComponent("../../Tokens.qml")
        if (c.status === Component.Error) {
            console.log(c.errorString())
            fail("Tokens.qml failed to load")
        }
        tk = c.createObject(this)
        verify(tk)
    }

    function cleanup() {
        if (tk) tk.destroy()
        tk = null
    }

    // parseColors feeds the hue table; coding resolves through blue →
    // color4 → engine default, corrected for contrast.
    function test_categoryHueChain() {
        tk.parseColors('background = "#000000"\nforeground = "#ffffff"\nblue = "#0000ff"\n')
        verify(tk.hues.blue === "#0000ff")
        verify(tk.categories.coding !== undefined)
        // Theme chain ran — the resolved hue is not the engine default.
        verify(tk.categories.coding !== tk.engineDefaults.coding)
    }

    // Missing named key falls through to the ANSI slot.
    function test_ansiFallback() {
        tk.parseColors('background = "#000000"\nforeground = "#ffffff"\ncolor4 = "#00aa00"\n')
        verify(tk.hues.color4 === "#00aa00")
        verify(tk.categories.coding !== undefined)
        // ANSI slot (green) resolved, not the blue engine default.
        verify(tk.categories.coding !== tk.engineDefaults.coding)
    }

    // R3 precedence: a user hex overrides the theme role entirely.
    function test_customHexWins() {
        var out = tk.categoryColor("coding", "#ff00aa", "blue")
        verify(out !== undefined && out !== "")
        verify(out !== tk.categories.coding)
    }

    // AE3: swap the staged colors.toml content and recompute — the
    // category hues must reflect the new palette, not the old.
    function test_reloadRecomputes() {
        tk.parseColors('background = "#000000"\nforeground = "#ffffff"\nblue = "#0000ff"\n')
        var before = tk.categories.coding
        tk.parseColors('background = "#000000"\nforeground = "#ffffff"\nblue = "#ff0000"\n')
        var after = tk.categories.coding
        verify(before !== after, "coding hue did not update after colors.toml swap")
    }
}
