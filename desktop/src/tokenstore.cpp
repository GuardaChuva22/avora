#include "tokenstore.h"

#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QSaveFile>
#include <QStandardPaths>

#include <windows.h>
#include <dpapi.h>

namespace {

QString path()
{
    return QStandardPaths::writableLocation(QStandardPaths::AppDataLocation) + "/token.dat";
}

constexpr wchar_t kDescription[] = L"Avora admin token";

} // namespace

namespace tokenstore {

QString load()
{
    QFile f(path());
    if (!f.open(QIODevice::ReadOnly))
        return {};
    QByteArray blob = f.readAll();
    DATA_BLOB in{static_cast<DWORD>(blob.size()), reinterpret_cast<BYTE *>(blob.data())};
    DATA_BLOB out{};
    if (!CryptUnprotectData(&in, nullptr, nullptr, nullptr, nullptr, CRYPTPROTECT_UI_FORBIDDEN, &out))
        return {};
    const QString token = QString::fromUtf8(reinterpret_cast<const char *>(out.pbData), static_cast<qsizetype>(out.cbData));
    SecureZeroMemory(out.pbData, out.cbData);
    LocalFree(out.pbData);
    return token;
}

void save(const QString &token)
{
    QByteArray plain = token.toUtf8();
    DATA_BLOB in{static_cast<DWORD>(plain.size()), reinterpret_cast<BYTE *>(plain.data())};
    DATA_BLOB out{};
    const bool ok = CryptProtectData(&in, kDescription, nullptr, nullptr, nullptr, CRYPTPROTECT_UI_FORBIDDEN, &out);
    SecureZeroMemory(plain.data(), static_cast<SIZE_T>(plain.size()));
    if (!ok)
        return;
    QDir().mkpath(QFileInfo(path()).absolutePath());
    QSaveFile f(path());
    if (f.open(QIODevice::WriteOnly)) {
        f.write(reinterpret_cast<const char *>(out.pbData), static_cast<qint64>(out.cbData));
        f.commit();
    }
    LocalFree(out.pbData);
}

void clear()
{
    QFile::remove(path());
}

} // namespace tokenstore
