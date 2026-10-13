#include "enginepath.h"
#include <QCoreApplication>
#include <QFileInfo>
#include <QStandardPaths>

QString resolveEngine() {
  const QString env = qEnvironmentVariable("DAYFLOW_ENGINE");
  if (!env.isEmpty() && QFileInfo(env).isExecutable()) return env;
  const QString beside = QCoreApplication::applicationDirPath() + QStringLiteral("/dayflow");
  if (QFileInfo(beside).isExecutable()) return beside;
  return QStandardPaths::findExecutable(QStringLiteral("dayflow"));
}
