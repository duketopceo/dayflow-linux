#pragma once
#include <QColor>
#include <QObject>
#include <QQmlEngine>

// qs.Commons.Color for the standalone host. This is a C++ singleton on
// purpose: Qt 6.12 added a built-in QtQuick `Color` singleton, and the tab
// QML does an unversioned `import QtQuick` (Omarchy's own files pin 6.11 to
// dodge this). A QML-file singleton loses that name clash; a C++-registered
// one wins it. Values mirror the fallback palette in
// /usr/share/omarchy/shell/Commons/Color.qml.
class ThemeColor : public QObject {
  Q_OBJECT
  QML_NAMED_ELEMENT(Color)
  QML_SINGLETON
  Q_PROPERTY(QColor foreground MEMBER m_foreground NOTIFY changed)
  Q_PROPERTY(QColor background MEMBER m_background NOTIFY changed)
  Q_PROPERTY(QColor accent MEMBER m_accent NOTIFY changed)
  Q_PROPERTY(QColor urgent MEMBER m_urgent NOTIFY changed)
  Q_PROPERTY(QColor muted MEMBER m_muted NOTIFY changed)
public:
  explicit ThemeColor(QObject *parent = nullptr) : QObject(parent) {}
signals:
  void changed();
private:
  QColor m_foreground{"#cacccc"}, m_background{"#101315"}, m_accent{"#cacccc"},
         m_urgent{"#a55555"}, m_muted{"#707880"};
};
