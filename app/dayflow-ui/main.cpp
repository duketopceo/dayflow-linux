#include "tray.h"
#include <QApplication>
#include <QIcon>
#include <QLocalServer>
#include <QLocalSocket>
#include <QLockFile>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickImageProvider>
#include <QStandardPaths>
#include <QDir>
#include <QProcess>
#include "enginepath.h"
#include <QQuickWindow>
#include <QTimer>

// image://icon/<theme-name> — backs Quickshell.iconPath() in the shim.
class IconProvider : public QQuickImageProvider {
public:
  IconProvider() : QQuickImageProvider(QQuickImageProvider::Pixmap) {}
  QPixmap requestPixmap(const QString &id, QSize *size, const QSize &req) override {
    const int s = req.width() > 0 ? req.width() : 32;
    QPixmap pm = QIcon::fromTheme(id).pixmap(s, s);
    if (size) *size = pm.size();
    return pm;
  }
};

int main(int argc, char *argv[]) {
  // Wayland app_id / portal attribution / SNI identity (plan KTD1).
  QGuiApplication::setDesktopFileName(QStringLiteral("dayflow"));
  QApplication app(argc, argv);
  app.setApplicationName(QStringLiteral("dayflow"));
  // QSettings home for window state: ~/.config/dayflow-ui/dayflow.conf.
  app.setOrganizationName(QStringLiteral("dayflow-ui"));
  app.setApplicationDisplayName(QStringLiteral("Dayflow"));
  app.setApplicationVersion(QStringLiteral(DAYFLOW_UI_VERSION));
  app.setQuitOnLastWindowClosed(false);

  // Single instance (plan KTD12). A second launch hands the running one a
  // "raise" over a local socket and exits, so "open Dayflow" from the bar or a
  // launcher brings the existing window up. A launch with --background (the
  // autostart entry) asks for nothing and just exits.
  const QString runDir = QStandardPaths::writableLocation(QStandardPaths::RuntimeLocation);
  const QString base = runDir.isEmpty() ? QDir::tempPath() : runDir;
  const QString sockName = base + QStringLiteral("/dayflow-ui.sock");
  QLockFile lock(base + QStringLiteral("/dayflow-ui.lock"));
  lock.setStaleLockTime(0);
  const bool multi = !qEnvironmentVariableIsEmpty("DAYFLOW_UI_ALLOW_MULTI");
  const bool background = app.arguments().contains(QStringLiteral("--background"));
  if (!multi && !lock.tryLock(100)) {
    if (!background) {
      QLocalSocket sock;
      sock.connectToServer(sockName);
      if (sock.waitForConnected(500)) {
        sock.write("raise\n");
        sock.waitForBytesWritten(500);
        sock.disconnectFromServer();
        return 0;
      }
    }
    qWarning("dayflow-ui: already running");
    return 0;
  }

  // The Panel fans out ~8 engine reads at once; against a never-opened DB
  // they race the first-run schema creation (some exit 1). Open it once,
  // serially, before the UI starts.
  if (const QString eng = resolveEngine(); !eng.isEmpty())
    {
      QProcess init;
      init.setStandardOutputFile(QProcess::nullDevice());
      init.start(eng, {QStringLiteral("status"), QStringLiteral("--json")});
      init.waitForFinished(10000);
    }

  QQmlApplicationEngine engine;
  engine.addImageProvider(QStringLiteral("icon"), new IconProvider);
  QObject::connect(&engine, &QQmlApplicationEngine::objectCreationFailed, &app,
                   [] { QCoreApplication::exit(70); }, Qt::QueuedConnection);

  const bool trayOk = QSystemTrayIcon::isSystemTrayAvailable();
  engine.rootContext()->setContextProperty(QStringLiteral("trayAvailable"), trayOk);
  engine.rootContext()->setContextProperty(QStringLiteral("initialTab"), qEnvironmentVariable("DAYFLOW_UI_TAB"));
  engine.rootContext()->setContextProperty(QStringLiteral("appExecPath"), QCoreApplication::applicationFilePath());
  // Test hook: semicolon-separated shell steps (see tests/nav.sh).
  engine.rootContext()->setContextProperty(QStringLiteral("uiScript"), qEnvironmentVariable("DAYFLOW_UI_SCRIPT"));
  // Test hook: DAYFLOW_UI_TESTQML=/path/Test.qml loads that file (component tests
  // that need the Process shim and the Dayflow.App module) instead of the window.
  if (const QString tq = qEnvironmentVariable("DAYFLOW_UI_TESTQML"); !tq.isEmpty()) {
    engine.load(QUrl::fromLocalFile(tq));
    return engine.rootObjects().isEmpty() ? 70 : app.exec();
  }
  engine.loadFromModule("Dayflow.Host", "Main");
  if (engine.rootObjects().isEmpty()) return 70;
  QObject *win = engine.rootObjects().first();

  Tray tray(win);
  QObject::connect(&tray, &Tray::quitRequested, &app, &QCoreApplication::quit);
  QObject::connect(win, SIGNAL(quitRequested()), &app, SLOT(quit()));

  // The running instance answers a second launch's "raise".
  QLocalServer raiseServer;
  if (!multi) {
    QLocalServer::removeServer(sockName);
    raiseServer.setSocketOptions(QLocalServer::UserAccessOption);
    if (raiseServer.listen(sockName)) {
      QObject::connect(&raiseServer, &QLocalServer::newConnection, &app, [&raiseServer, win] {
        while (QLocalSocket *c = raiseServer.nextPendingConnection()) {
          QObject::connect(c, &QLocalSocket::readyRead, c, [c, win] {
            if (c->readAll().contains("raise")) {
              qInfo("dayflow-ui: raise requested");
              QMetaObject::invokeMethod(win, "showAndRaise");
            }
          });
          QObject::connect(c, &QLocalSocket::disconnected, c, &QObject::deleteLater);
        }
      });
    } else {
      qWarning("dayflow-ui: raise socket unavailable: %s", qPrintable(raiseServer.errorString()));
    }
  }

  // With no tray host, a hidden window would be unreachable: always show.
  if (!background || !trayOk) QMetaObject::invokeMethod(win, "showAndRaise");
  qInfo("dayflow-ui %s ready (tray=%s)", DAYFLOW_UI_VERSION, trayOk ? "yes" : "no");

  // Dev/test hook: DAYFLOW_UI_SCREENSHOT=/path.png grabs the window after 3s and quits.
  const QString shot = qEnvironmentVariable("DAYFLOW_UI_SCREENSHOT");
  if (!shot.isEmpty()) {
    QTimer::singleShot(3000, &app, [win, shot] {
      if (auto *w = qobject_cast<QQuickWindow *>(win)) w->grabWindow().save(shot);
      QCoreApplication::quit();
    });
  }
  return app.exec();
}
