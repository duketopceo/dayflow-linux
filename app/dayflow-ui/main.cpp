#include "tray.h"
#include <QApplication>
#include <QIcon>
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
  app.setApplicationDisplayName(QStringLiteral("Dayflow"));
  app.setApplicationVersion(QStringLiteral(DAYFLOW_UI_VERSION));
  app.setQuitOnLastWindowClosed(false);

  // Single instance (plan KTD12): a second launch just exits; the user raises
  // the window from the tray. (Raise-over-IPC lands with the D-Bus work.)
  const QString runDir = QStandardPaths::writableLocation(QStandardPaths::RuntimeLocation);
  QLockFile lock((runDir.isEmpty() ? QDir::tempPath() : runDir) + QStringLiteral("/dayflow-ui.lock"));
  lock.setStaleLockTime(0);
  if (qEnvironmentVariableIsEmpty("DAYFLOW_UI_ALLOW_MULTI") && !lock.tryLock(100)) {
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
  engine.loadFromModule("Dayflow.Host", "Main");
  if (engine.rootObjects().isEmpty()) return 70;
  QObject *win = engine.rootObjects().first();

  Tray tray(win);
  QObject::connect(&tray, &Tray::quitRequested, &app, &QCoreApplication::quit);
  QObject::connect(win, SIGNAL(quitRequested()), &app, SLOT(quit()));

  const bool background = app.arguments().contains(QStringLiteral("--background"));
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
