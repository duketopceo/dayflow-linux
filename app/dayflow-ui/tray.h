#pragma once
#include <QObject>
#include <QSystemTrayIcon>
#include <QTimer>

class QMenu;
class QAction;
class QProcess;
class QQuickWindow;

// StatusNotifierItem tray (QSystemTrayIcon speaks SNI on Wayland/KDE/GNOME
// with the appindicator extension). Icon swaps with the engine's
// capture_state; polls `dayflow status --json` — read-only, never starts
// capture.
class Tray : public QObject {
  Q_OBJECT
public:
  enum State { Unknown, Recording, Paused, Down };
  explicit Tray(QObject *window, QObject *parent = nullptr);
  bool available() const { return m_available; }
  State state() const { return m_state; }
signals:
  void quitRequested();
private slots:
  void poll();
  void toggleCapture();
private:
  void setState(State s, const QString &detail);
  QIcon iconFor(State s) const;
  QObject *m_window;
  QSystemTrayIcon *m_tray = nullptr;
  QMenu *m_menu = nullptr;
  QAction *m_showAct = nullptr, *m_toggleAct = nullptr, *m_statusAct = nullptr;
  QTimer m_timer;
  QProcess *m_poll = nullptr;
  State m_state = Unknown;
  bool m_available = false;
};
