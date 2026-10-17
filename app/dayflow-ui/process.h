#pragma once
#include <QByteArray>
#include <QObject>
#include <QProcess>
#include <QQmlEngine>
#include <QStringList>

// Contract-exact stand-in for Quickshell.Io.Process / StdioCollector /
// SplitParser (plan KTD11). The reused tab QML depends on these semantics:
//   * `command` is assignable while stopped; `running = true` starts it.
//   * FailedToStart emits NEITHER exited nor streamFinished — callers'
//     `didStart` pattern relies on that.
//   * On a normal finish, parsers are drained and streamFinished fires
//     BEFORE exited, so both handler styles see complete output.
//   * `stdinEnabled` + write() feed stdin; stdin closes when the process
//     is stopped or write is followed by endInput semantics of the caller.

class DataStreamParser : public QObject {
  Q_OBJECT
  QML_ELEMENT
  QML_UNCREATABLE("base class")
public:
  using QObject::QObject;
  virtual void feed(const QByteArray &) {}
  virtual void finish() {}
  virtual void reset() {}
};

class StdioCollector : public DataStreamParser {
  Q_OBJECT
  QML_ELEMENT
  Q_PROPERTY(bool waitForEnd READ waitForEnd WRITE setWaitForEnd NOTIFY waitForEndChanged)
  Q_PROPERTY(QString text READ text NOTIFY textChanged)
  Q_PROPERTY(QByteArray data READ data NOTIFY textChanged)
public:
  explicit StdioCollector(QObject *parent = nullptr) : DataStreamParser(parent) {}
  bool waitForEnd() const { return m_waitForEnd; }
  void setWaitForEnd(bool v);
  QString text() const { return QString::fromUtf8(m_published); }
  QByteArray data() const { return m_published; }
  void feed(const QByteArray &d) override;
  void finish() override;
  void reset() override;
signals:
  void waitForEndChanged();
  void textChanged();
  void streamFinished();
private:
  bool m_waitForEnd = true;
  QByteArray m_buf, m_published;
};

class SplitParser : public DataStreamParser {
  Q_OBJECT
  QML_ELEMENT
  Q_PROPERTY(QString splitMarker READ splitMarker WRITE setSplitMarker NOTIFY splitMarkerChanged)
public:
  explicit SplitParser(QObject *parent = nullptr) : DataStreamParser(parent) {}
  QString splitMarker() const { return m_marker; }
  void setSplitMarker(const QString &m) { if (m != m_marker) { m_marker = m; emit splitMarkerChanged(); } }
  void feed(const QByteArray &d) override;
  void finish() override;
  void reset() override { m_buf.clear(); }
signals:
  void splitMarkerChanged();
  void read(const QString &data);
private:
  QString m_marker = QStringLiteral("\n");
  QByteArray m_buf;
};

class Process : public QObject {
  Q_OBJECT
  QML_ELEMENT
  Q_PROPERTY(QStringList command READ command WRITE setCommand NOTIFY commandChanged)
  Q_PROPERTY(bool running READ running WRITE setRunning NOTIFY runningChanged)
  Q_PROPERTY(bool stdinEnabled READ stdinEnabled WRITE setStdinEnabled NOTIFY stdinEnabledChanged)
  Q_PROPERTY(QString workingDirectory READ workingDirectory WRITE setWorkingDirectory NOTIFY workingDirectoryChanged)
  Q_PROPERTY(QVariantMap environment READ environment WRITE setEnvironment NOTIFY environmentChanged)
  Q_PROPERTY(DataStreamParser *stdout READ outParser WRITE setOutParser NOTIFY outParserChanged)
  Q_PROPERTY(DataStreamParser *stderr READ errParser WRITE setErrParser NOTIFY errParserChanged)
  Q_PROPERTY(qint64 pid READ pid NOTIFY runningChanged)
public:
  explicit Process(QObject *parent = nullptr);
  ~Process() override;

  QStringList command() const { return m_command; }
  void setCommand(const QStringList &c);
  bool running() const { return m_running; }
  void setRunning(bool r);
  bool stdinEnabled() const { return m_stdinEnabled; }
  void setStdinEnabled(bool v) { if (v != m_stdinEnabled) { m_stdinEnabled = v; emit stdinEnabledChanged(); } }
  QString workingDirectory() const { return m_cwd; }
  void setWorkingDirectory(const QString &d) { if (d != m_cwd) { m_cwd = d; emit workingDirectoryChanged(); } }
  QVariantMap environment() const { return m_env; }
  void setEnvironment(const QVariantMap &e) { m_env = e; emit environmentChanged(); }
  DataStreamParser *outParser() const { return m_out; }
  void setOutParser(DataStreamParser *p);
  DataStreamParser *errParser() const { return m_err; }
  void setErrParser(DataStreamParser *p);
  qint64 pid() const { return m_proc ? m_proc->processId() : 0; }

  Q_INVOKABLE void write(const QString &data);
  Q_INVOKABLE void startDetached();

signals:
  void commandChanged();
  void runningChanged();
  void stdinEnabledChanged();
  void workingDirectoryChanged();
  void environmentChanged();
  void outParserChanged();
  void errParserChanged();
  void started();
  void exited(int exitCode, int exitStatus);

private:
  void start();
  void stop();
  void drain();
  void onFinished(int code, QProcess::ExitStatus st);
  void setRunningInternal(bool r);
  QProcessEnvironment childEnv() const;

  QStringList m_command;
  QProcess *m_proc = nullptr;
  bool m_running = false;
  bool m_stdinEnabled = false;
  bool m_clipboardPipe = false;
  QByteArray m_clipBuf;
  QString m_cwd;
  QVariantMap m_env;
  DataStreamParser *m_out = nullptr;
  DataStreamParser *m_err = nullptr;
};
