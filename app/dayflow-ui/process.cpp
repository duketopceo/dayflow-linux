#include "process.h"
#include "enginepath.h"
#include <QClipboard>
#include <QDir>
#include <QGuiApplication>

// ---- StdioCollector --------------------------------------------------------
void StdioCollector::setWaitForEnd(bool v) {
  if (v == m_waitForEnd) return;
  m_waitForEnd = v;
  emit waitForEndChanged();
}
void StdioCollector::feed(const QByteArray &d) {
  m_buf += d;
  if (!m_waitForEnd) { m_published = m_buf; emit textChanged(); }
}
void StdioCollector::finish() {
  m_published = m_buf;
  emit textChanged();
  emit streamFinished();
}
void StdioCollector::reset() {
  m_buf.clear();
  if (!m_published.isEmpty()) { m_published.clear(); emit textChanged(); }
}

// ---- SplitParser -----------------------------------------------------------
void SplitParser::feed(const QByteArray &d) {
  m_buf += d;
  const QByteArray marker = m_marker.toUtf8();
  if (marker.isEmpty()) return;
  int idx;
  while ((idx = m_buf.indexOf(marker)) >= 0) {
    const QByteArray line = m_buf.left(idx);
    m_buf.remove(0, idx + marker.size());
    emit read(QString::fromUtf8(line));
  }
}
void SplitParser::finish() {
  if (!m_buf.isEmpty()) { const QByteArray rest = m_buf; m_buf.clear(); emit read(QString::fromUtf8(rest)); }
}

// ---- Process -----------------------------------------------------------------
Process::Process(QObject *parent) : QObject(parent) {}
Process::~Process() {
  if (m_proc) {
    m_proc->disconnect(this);
    m_proc->kill();
    m_proc->waitForFinished(500);
  }
}

void Process::setCommand(const QStringList &c) {
  if (c == m_command) return;
  m_command = c;
  emit commandChanged();
}
void Process::setOutParser(DataStreamParser *p) { if (p != m_out) { m_out = p; emit outParserChanged(); } }
void Process::setErrParser(DataStreamParser *p) { if (p != m_err) { m_err = p; emit errParserChanged(); } }

void Process::setRunning(bool r) {
  if (r) { if (!m_running) start(); }
  else if (m_running) stop();
}

void Process::setRunningInternal(bool r) {
  if (r == m_running) return;
  m_running = r;
  emit runningChanged();
}

QProcessEnvironment Process::childEnv() const {
  QProcessEnvironment env = QProcessEnvironment::systemEnvironment();
  // Anything that says `dayflow` (argv0 or inside `bash -c`) must hit the
  // bundled engine, never a stale PATH copy (plan KTD5).
  const QString engine = resolveEngine();
  if (!engine.isEmpty()) {
    const QString dir = QFileInfo(engine).absolutePath();
    env.insert(QStringLiteral("PATH"), dir + QLatin1Char(':') + env.value(QStringLiteral("PATH")));
  }
  for (auto it = m_env.cbegin(); it != m_env.cend(); ++it)
    env.insert(it.key(), it.value().toString());
  return env;
}

void Process::start() {
  if (m_command.isEmpty()) return;
  QStringList argv = m_command;
  m_clipboardPipe = false;
  m_clipBuf.clear();

  // The reused QML shells out `bash -c "dayflow … | wl-copy"` in six places.
  // wl-copy is wlroots/Wayland-only; route those to the Qt clipboard instead.
  static const QString kPipe = QStringLiteral(" | wl-copy");
  if (argv.size() == 3 && argv[0] == QLatin1String("bash") && argv[1] == QLatin1String("-c")
      && argv[2].endsWith(kPipe)) {
    argv[2].chop(kPipe.size());
    m_clipboardPipe = true;
  } else if (argv[0] == QLatin1String("dayflow")) {
    const QString engine = resolveEngine();
    if (!engine.isEmpty()) argv[0] = engine;
  }

  if (m_proc) { m_proc->disconnect(this); m_proc->deleteLater(); }
  m_proc = new QProcess(this);
  m_proc->setProcessEnvironment(childEnv());
  if (!m_cwd.isEmpty()) m_proc->setWorkingDirectory(m_cwd);
  if (m_out) m_out->reset();
  if (m_err) m_err->reset();

  connect(m_proc, &QProcess::readyReadStandardOutput, this, [this] {
    const QByteArray d = m_proc->readAllStandardOutput();
    if (m_clipboardPipe) m_clipBuf += d;
    if (m_out) m_out->feed(d);
  });
  connect(m_proc, &QProcess::readyReadStandardError, this, [this] {
    const QByteArray d = m_proc->readAllStandardError();
    if (m_err) m_err->feed(d);
  });
  connect(m_proc, &QProcess::started, this, [this] {
    setRunningInternal(true);
    emit started();
  });
  connect(m_proc, &QProcess::errorOccurred, this, [this](QProcess::ProcessError e) {
    // FailedToStart: not running, and neither exited nor streamFinished.
    if (e == QProcess::FailedToStart) setRunningInternal(false);
  });
  connect(m_proc, &QProcess::finished, this, &Process::onFinished);

  const QString program = argv.takeFirst();
  m_proc->start(program, argv, m_stdinEnabled ? QIODevice::ReadWrite : QIODevice::ReadOnly);
  if (!m_stdinEnabled) m_proc->closeWriteChannel();
}

void Process::stop() {
  if (!m_proc) return;
  m_proc->terminate();
  if (!m_proc->waitForFinished(1500)) m_proc->kill();
}

void Process::drain() {
  if (!m_proc) return;
  const QByteArray o = m_proc->readAllStandardOutput();
  if (!o.isEmpty()) { if (m_clipboardPipe) m_clipBuf += o; if (m_out) m_out->feed(o); }
  const QByteArray e = m_proc->readAllStandardError();
  if (!e.isEmpty() && m_err) m_err->feed(e);
}

void Process::onFinished(int code, QProcess::ExitStatus st) {
  drain();
  if (m_clipboardPipe && code == 0 && qGuiApp && qGuiApp->clipboard())
    qGuiApp->clipboard()->setText(QString::fromUtf8(m_clipBuf));
  if (m_out) m_out->finish();
  if (m_err) m_err->finish();
  setRunningInternal(false);
  emit exited(code, int(st));
}

void Process::write(const QString &data) {
  if (m_proc && m_stdinEnabled && m_proc->state() == QProcess::Running) {
    m_proc->write(data.toUtf8());
    // Quickshell callers write a whole payload then expect EOF semantics
    // from the child (config patch / key set read stdin to end).
    m_proc->closeWriteChannel();
  }
}

void Process::startDetached() {
  if (m_command.isEmpty()) return;
  QStringList argv = m_command;
  const QString program = argv.takeFirst();
  QProcess p;
  p.setProgram(program == QLatin1String("dayflow") && !resolveEngine().isEmpty() ? resolveEngine() : program);
  p.setArguments(argv);
  p.setProcessEnvironment(childEnv());
  p.startDetached();
}
