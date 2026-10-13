#pragma once
#include <QString>

// Engine resolution (plan KTD5): DAYFLOW_ENGINE override, then the binary
// bundled beside dayflow-ui, then PATH. Returns "" when none is found.
QString resolveEngine();
