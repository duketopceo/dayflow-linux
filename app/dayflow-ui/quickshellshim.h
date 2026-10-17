#pragma once
#include <QObject>
#include <QQmlEngine>

// Shim for the `Quickshell` singleton the tab QML calls. Only iconPath() is
// used by the reused QML (Panel.qml appIcon()).
class QuickshellShim : public QObject {
  Q_OBJECT
  QML_NAMED_ELEMENT(Quickshell)
  QML_SINGLETON
public:
  explicit QuickshellShim(QObject *parent = nullptr) : QObject(parent) {}
  // Returns an image:// URL served by IconProvider, or "" when the theme has
  // no such icon (callers fall through to the next candidate name).
  Q_INVOKABLE QString iconPath(const QString &name, bool check = false) const;
};
