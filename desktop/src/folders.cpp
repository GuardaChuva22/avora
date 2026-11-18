// Project folders on this PC: the list the local Avora runtime accepts and
// the AGENTS.md inside them. Only folders the owner picked here are ever
// written to.
#include "api.h"

#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QSaveFile>
#include <QUrl>

namespace {

QString foldersFile()
{
    const QString base = qEnvironmentVariable("APPDATA");
    if (base.isEmpty())
        return {};
    return QDir(base).filePath(QStringLiteral("Avora/folders.json"));
}

QStringList allowedFolders()
{
    QFile f(foldersFile());
    if (!f.open(QIODevice::ReadOnly))
        return {};
    QStringList out;
    for (const auto &v : QJsonDocument::fromJson(f.readAll()).array())
        if (v.isString())
            out << v.toString();
    return out;
}

QString clean(const QString &folder)
{
    if (folder.trimmed().isEmpty())
        return {};
    const QFileInfo info(folder.trimmed());
    if (!info.isAbsolute())
        return {};
    return QDir::toNativeSeparators(QDir::cleanPath(info.absoluteFilePath()));
}

bool allowed(const QString &folder)
{
    const QString want = clean(folder);
    if (want.isEmpty())
        return false;
    for (const QString &a : allowedFolders()) {
        const QString root = clean(a);
        if (root.isEmpty())
            continue;
        if (want.compare(root, Qt::CaseInsensitive) == 0 ||
            want.startsWith(root + QDir::separator(), Qt::CaseInsensitive) ||
            (root.endsWith(QDir::separator()) && want.startsWith(root, Qt::CaseInsensitive)))
            return true;
    }
    return false;
}

} // namespace

QString Api::localPath(const QUrl &url) const
{
    return url.isLocalFile() ? QDir::toNativeSeparators(QDir::cleanPath(url.toLocalFile())) : QString();
}

bool Api::folderExists(const QString &folder) const
{
    const QString path = clean(folder);
    return !path.isEmpty() && QFileInfo(path).isDir();
}

bool Api::allowFolder(const QString &folder) const
{
    const QString path = clean(folder);
    const QString file = foldersFile();
    if (path.isEmpty() || file.isEmpty() || !QFileInfo(path).isDir())
        return false;
    if (allowed(path))
        return true;
    QStringList list = allowedFolders();
    list << path;
    QJsonArray arr;
    for (const QString &p : list)
        arr.append(p);
    QDir().mkpath(QFileInfo(file).absolutePath());
    QSaveFile out(file);
    if (!out.open(QIODevice::WriteOnly))
        return false;
    out.write(QJsonDocument(arr).toJson(QJsonDocument::Indented));
    return out.commit();
}

QString Api::readAgentsFile(const QString &folder) const
{
    if (!allowed(folder))
        return {};
    QFile f(QDir(clean(folder)).filePath(QStringLiteral("AGENTS.md")));
    if (!f.open(QIODevice::ReadOnly))
        return {};
    return QString::fromUtf8(f.read(80 * 1024)).trimmed();
}

bool Api::writeAgentsFile(const QString &folder, const QString &text) const
{
    if (!allowed(folder) || text.trimmed().isEmpty())
        return false;
    QSaveFile f(QDir(clean(folder)).filePath(QStringLiteral("AGENTS.md")));
    if (!f.open(QIODevice::WriteOnly))
        return false;
    f.write(text.trimmed().toUtf8() + "\n");
    return f.commit();
}
