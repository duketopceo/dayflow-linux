import QtQuick
// Test stub — FileView with staged text so Tokens reload/onLoaded can be driven.
QtObject {
  property string path: ""
  property bool watchChanges: false
  property bool printErrors: false
  property string stagedText: ""
  signal loaded()
  signal fileChanged()
  signal loadFailed()
  function text() { return stagedText }
  function reload() {
    if (stagedText !== undefined && stagedText !== null)
      loaded()
    else
      loadFailed()
  }
}
