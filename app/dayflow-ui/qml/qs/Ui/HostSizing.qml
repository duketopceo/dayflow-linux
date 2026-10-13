pragma Singleton
import QtQuick

// Channel from the KeyboardPanel shim to the host window: the content size
// Panel.qml asked for (it flips between compact and expanded widths).
QtObject {
  property int width: 0
  property int height: 0
}
