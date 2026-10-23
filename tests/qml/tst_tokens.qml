// tst_tokens.qml — AE3: Tokens re-parses a staged colors.toml swap and
// recomputes roles. Runs under qmltestrunner with the qs/Commons stub.
// Run: qmltestrunner -input tests/qml/tst_tokens.qml -import tests/qml
import QtQuick
import QtTest
import "../.."
import "../../ThemeMath.js" as TM

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
        var expected = TM.correctContrast("#0000ff", tk.bg, tk.fg, 3.0, 0.85)
        compare(tk.categories.coding, expected)
    }

    // Missing named key falls through to the ANSI slot.
    function test_ansiFallback() {
        tk.parseColors('background = "#000000"\nforeground = "#ffffff"\ncolor4 = "#00aa00"\n')
        verify(tk.hues.color4 === "#00aa00")
        var expected = TM.correctContrast("#00aa00", tk.bg, tk.fg, 3.0, 0.85)
        compare(tk.categories.coding, expected)
    }

    // R3 precedence: a user hex overrides the theme role entirely.
    function test_customHexWins() {
        var out = tk.categoryColor("coding", "#ff00aa", "blue")
        verify(out !== undefined && out !== "")
        verify(out !== tk.categories.coding)
    }

    // AE3: stage colors.toml text on the FileView stub, reload, then
    // fire onFileChanged so the reload path updates categories.
    function test_reloadRecomputes() {
        // colorsFile is the second FileView child after themeNameFile.
        var files = []
        for (var j = 0; j < tk.children.length; j++) {
            if (tk.children[j].reload && tk.children[j].stagedText !== undefined)
                files.push(tk.children[j])
        }
        verify(files.length >= 2, "expected theme + colors FileView stubs")
        var colors = files[1]
        colors.stagedText = 'background = "#000000"\nforeground = "#ffffff"\nblue = "#0000ff"\n'
        colors.reload()
        var before = tk.categories.coding
        colors.stagedText = 'background = "#000000"\nforeground = "#ffffff"\nblue = "#ff0000"\n'
        colors.fileChanged()
        var after = tk.categories.coding
        verify(before !== after, "coding hue did not update after colors.toml reload")
    }
}
