#!/usr/bin/env python3
"""Record a real program's output for the render corpus: the exact bytes it writes to its
terminal, at a fixed size, replayable into any terminal of that size.

    record.py start <scratch> <name> <cols> <rows> <command...>   # runs it on a pty of that size
    record.py send <scratch> <name> <text>                         # types text (\\r is Enter, \\x1b Esc)
    record.py stop <scratch> <name> [what]                         # ends it, adds corpus.json

The program runs on a pseudo-terminal of its own with TERM=xterm-256color and
COLORTERM=truecolor and nothing else of your shell's environment beyond HOME, PATH,
USER, LANG and SHELL, so it writes what it would to a phone terminal. <scratch> is a directory
of yours that holds the recorder's pid and key pipe while it runs.
"""
import fcntl
import json
import os
import pathlib
import pty
import select
import signal
import struct
import subprocess
import sys
import termios
import time

HERE = pathlib.Path(__file__).resolve().parent


def paths(scratch: str, name: str):
    base = pathlib.Path(scratch)
    return base / f"{name}.pid", base / f"{name}.keys", base / f"{name}.size"


def record(scratch: str, name: str, cols: int, rows: int, command: list[str]) -> None:
    pid_file, keys, size_file = paths(scratch, name)
    pid, fd = pty.fork()
    if pid == 0:
        # Only what a fresh login gives a program: nothing of the shell that runs the recorder.
        env = {k: os.environ[k] for k in ("HOME", "PATH", "USER", "LANG", "SHELL") if k in os.environ}
        env.update(TERM="xterm-256color", COLORTERM="truecolor", COLUMNS=str(cols), LINES=str(rows))
        time.sleep(0.5)
        os.execvpe(command[0], command, env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    os.kill(pid, signal.SIGWINCH)
    size_file.write_text(f"{cols} {rows}\n")
    key_fd = os.open(keys, os.O_RDWR | os.O_NONBLOCK)
    stop = False

    def finish(*_):
        nonlocal stop
        stop = True

    signal.signal(signal.SIGTERM, finish)
    with open(HERE / f"{name}.vt", "wb") as out:
        while not stop:
            try:
                ready, _, _ = select.select([fd, key_fd], [], [], 0.2)
            except InterruptedError:
                continue
            if fd in ready:
                try:
                    data = os.read(fd, 65536)
                except OSError:
                    break
                if not data:
                    break
                out.write(data)
                out.flush()
            if key_fd in ready:
                os.write(fd, os.read(key_fd, 65536))
    try:
        os.kill(pid, signal.SIGHUP)
    except ProcessLookupError:
        pass
    pid_file.unlink(missing_ok=True)


def main() -> int:
    if len(sys.argv) < 4 or sys.argv[1] not in ("start", "send", "stop"):
        print(__doc__, file=sys.stderr)
        return 1
    cmd, scratch, name = sys.argv[1:4]
    pid_file, keys, size_file = paths(scratch, name)
    if cmd == "start":
        cols, rows, command = int(sys.argv[4]), int(sys.argv[5]), sys.argv[6:]
        if not command:
            print("start needs a command", file=sys.stderr)
            return 1
        os.mkfifo(keys)
        recorder = subprocess.Popen(
            [sys.executable, "-I", __file__, "_record", scratch, name, str(cols), str(rows), *command],
            start_new_session=True, stdin=subprocess.DEVNULL,
        )
        pid_file.write_text(f"{recorder.pid}\n")
        return 0
    if cmd == "send":
        text = sys.argv[4].encode().decode("unicode_escape").encode("latin-1")
        with open(keys, "wb") as pipe:
            pipe.write(text)
        return 0
    what = sys.argv[4] if len(sys.argv) > 4 else "recorded"
    os.kill(int(pid_file.read_text()), signal.SIGTERM)
    for _ in range(50):
        if not pid_file.exists():
            break
        time.sleep(0.1)
    cols, rows = size_file.read_text().split()
    size_file.unlink(missing_ok=True)
    keys.unlink(missing_ok=True)
    corpus = HERE / "corpus.json"
    entries = json.loads(corpus.read_text())
    entries[name] = {"cols": int(cols), "rows": int(rows), "what": what, "source": "record.py"}
    corpus.write_text(json.dumps(dict(sorted(entries.items())), indent=2, ensure_ascii=False) + "\n")
    return 0


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "_record":
        record(sys.argv[2], sys.argv[3], int(sys.argv[4]), int(sys.argv[5]), sys.argv[6:])
        sys.exit(0)
    sys.exit(main())
