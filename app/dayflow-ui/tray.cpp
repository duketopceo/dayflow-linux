#include "tray.h"
#include "enginepath.h"
#include <QAction>
#include <QJsonDocument>
#include <QJsonObject>
#include <QMenu>
#include <QMetaObject>
#include <QPainter>
#include <QPixmap>
#include <QProcess>

Tray::Tray(QObject *window, QObject *parent) : QObject(parent), m_window(window) {
  m_available = QSystemTrayIcon::isSystemTrayAvailable();
  m_menu = new QMenu();
  m_statusAct = m_menu->addAction(tr("Dayflow"));
  m_statusAct->setEnabled(false);
  m_menu->addSeparator();
  m_showAct = m_menu->addAction(tr("Show / hide window"));
  connect(m_showAct, &QAction::triggered, this, [this] {
    QMetaObject::invokeMethod(m_window, "toggleVisible");
  });
  m_toggleAct = m_menu->addAction(tr("Pause recording"));
  connect(m_toggleAct, &QAction::triggered, this, &Tray::toggleCapture);
  m_menu->addSeparator();
  connect(m_menu->addAction(tr("Quit")), &QAction::triggered, this, &Tray::quitRequested);

  m_tray = new QSystemTrayIcon(this);
  m_tray->setContextMenu(m_menu);
  m_tray->setIcon(iconFor(Unknown));
  m_tray->setToolTip(QStringLiteral("Dayflow"));
  connect(m_tray, &QSystemTrayIcon::activated, this, [this](QSystemTrayIcon::ActivationReason r) {
    if (r == QSystemTrayIcon::Trigger) QMetaObject::invokeMethod(m_window, "toggleVisible");
  });
  if (m_available) m_tray->show();

  m_timer.setInterval(15000);
  connect(&m_timer, &QTimer::timeout, this, &Tray::poll);
  m_timer.start();
  poll();
}

QIcon Tray::iconFor(State s) const {
  QPixmap pm(64, 64);
  pm.fill(Qt::transparent);
  QPainter p(&pm);
  p.setRenderHint(QPainter::Antialiasing);
  QColor c = s == Recording ? QColor("#4caf6e") : s == Paused ? QColor("#d9a441")
           : s == Down ? QColor("#a55555") : QColor("#707880");
  p.setPen(QPen(QColor("#cacccc"), 5));
  p.setBrush(Qt::NoBrush);
  p.drawEllipse(QRectF(6, 6, 52, 52));
  p.setPen(Qt::NoPen);
  p.setBrush(c);
  p.drawEllipse(QRectF(20, 20, 24, 24));
  return QIcon(pm);
}

void Tray::setState(State s, const QString &detail) {
  m_state = s;
  m_tray->setIcon(iconFor(s));
  m_tray->setToolTip(QStringLiteral("Dayflow — ") + detail);
  m_statusAct->setText(QStringLiteral("Dayflow — ") + detail);
  m_toggleAct->setText(s == Paused ? tr("Resume recording") : tr("Pause recording"));
  m_toggleAct->setEnabled(s == Recording || s == Paused);
}

void Tray::poll() {
  if (m_poll) return;
  const QString engine = resolveEngine();
  if (engine.isEmpty()) { setState(Down, tr("engine not found")); return; }
  m_poll = new QProcess(this);
  connect(m_poll, &QProcess::finished, this, [this] {
    const QJsonObject o = QJsonDocument::fromJson(m_poll->readAllStandardOutput()).object();
    m_poll->deleteLater();
    m_poll = nullptr;
    if (o.isEmpty()) { setState(Down, tr("engine not answering")); return; }
    const QString cs = o.value("capture_state").toString();
    if (o.value("paused").toBool()) setState(Paused, tr("paused"));
    else if (cs == "down") setState(Down, tr("capture is not running"));
    else setState(Recording, tr("recording"));
  });
  connect(m_poll, &QProcess::errorOccurred, this, [this](QProcess::ProcessError) {
    if (!m_poll) return;
    m_poll->deleteLater();
    m_poll = nullptr;
    setState(Down, tr("engine not answering"));
  });
  m_poll->start(engine, {QStringLiteral("status"), QStringLiteral("--json")});
}

void Tray::toggleCapture() {
  const QString engine = resolveEngine();
  if (engine.isEmpty()) return;
  QProcess::startDetached(engine, {QStringLiteral("toggle")});
  QTimer::singleShot(1500, this, &Tray::poll);
}
