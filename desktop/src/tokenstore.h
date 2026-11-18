#pragma once

#include <QString>

// The admin token is kept encrypted with Windows DPAPI, readable only by this
// Windows user on this PC. Never written in plain text, never logged.
namespace tokenstore {
QString load();
void save(const QString &token);
void clear();
} // namespace tokenstore
