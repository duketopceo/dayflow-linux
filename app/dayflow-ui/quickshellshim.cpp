#include "quickshellshim.h"
#include <QIcon>

QString QuickshellShim::iconPath(const QString &name, bool check) const {
  if (name.isEmpty()) return {};
  if (check && !QIcon::hasThemeIcon(name)) return {};
  return QStringLiteral("image://icon/") + name;
}
