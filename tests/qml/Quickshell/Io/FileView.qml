import QtQuick
// Test stub — inert FileView. Tokens calls text()/reload() and attaches
// onLoaded/onFileChanged/onLoadFailed; tests drive parseColors directly.
QtObject {
  property string path: ""
  property bool watchChanges: false
  property bool printErrors: false
  signal loaded()
  signal fileChanged()
  signal loadFailed()
  function text() { return "" }
  function reload() { loadFailed() }
}
