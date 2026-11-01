#!/usr/bin/env python3
"""Exercise two real daemons with inert providers, never the user's daemon.

Build cmd/mewla into MEWLA_BUILD_TMPDIR, then run from the repository:
  python3 scripts/tests/computer-reply-routing.py /absolute/path/to/mewla
Requires Linux, cc and tmux. All state, sockets and input captures are temporary.
The fixture completes each synthetic turn before the next input: this tests
idle Session wake-up without pretending to provide live provider activity.
"""

import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import tempfile
import time


FIXTURE = r'''
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <signal.h>
int main(int argc, char **argv) {
  for (int i = 1; i < argc; i++) {
    if (!strcmp(argv[i], "--version")) { puts("grok 1.0.0"); return 0; }
  }
  signal(SIGINT, SIG_IGN);
  char path[2048];
  const char *worker = getenv("MEWLA_WORKER_ID");
  snprintf(path, sizeof(path), "%s/%s.input", getenv("ROUTING_FIXTURE_DIR"),
           worker ? worker : "brain");
  FILE *f = fopen(path, "a");
  if (!f) return 2;
  setbuf(f, NULL);
  puts("Grok 4.5 · always-approve\n❯ ");
  fflush(stdout);
  int c;
  while ((c = getchar()) != EOF) fputc(c, f);
  return 0;
}
'''


def wait(what, ready, timeout=60):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = ready()
        if result:
            return result
        time.sleep(0.1)
    raise AssertionError("Timed out: " + what)


class Daemon:
    def __init__(self, root, name, binary):
        self.name, self.binary = name, str(binary)
        self.state = root / name
        self.state.mkdir()
        self.socket = root / (name + ".sock")
        self.processes, self.logs, self.turns = [], [], {}
        self.env = {
            k: v for k, v in os.environ.items()
            if not k.startswith("MEWLA_") and k not in ("TMUX", "TMUX_PANE")
        }
        self.env.update(
            MEWLA_STATE_ROOT=str(self.state), MEWLA_STATE_DIR=str(self.state),
            MEWLA_BRAIN_HOST_EXECUTOR="grok", TMUX=str(self.socket) + ",0,0",
            PATH="/usr/bin:/bin", ZDOTDIR=str(root), ROUTING_FIXTURE_DIR=str(self.state),
        )
        (self.state / "executors.toml").write_text(
            '[[executors]]\nname="grok"\nkind="grok"\ncommand='
            + json.dumps(str(root / "grok")) + "\n"
        )
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            self.port = listener.getsockname()[1]

    def start(self):
        subprocess.run(
            ["/usr/bin/tmux", "-S", str(self.socket), "-f", "/dev/null",
             "new-session", "-d", "-s", "keeper", "/bin/cat"], check=True,
        )
        self.restart()

    def restart(self):
        log = open(self.state / ("daemon-%d.log" % len(self.processes)), "w")
        self.logs.append(log)
        process = subprocess.Popen(
            [self.binary, "-addr", "127.0.0.1:" + str(self.port)],
            env=self.env, stdout=log, stderr=log,
        )
        self.processes.append(process)
        self.process = process

        def ready():
            if process.poll() is not None:
                raise RuntimeError(self.name + " exited: " + Path(log.name).read_text())
            try:
                return self.control({"type": "computers", "computers_request": {"action": "list"}})
            except (FileNotFoundError, ConnectionRefusedError, ConnectionResetError):
                return False

        wait(self.name + " control socket", ready)

    def control(self, request):
        with socket.socket(socket.AF_UNIX) as connection:
            connection.settimeout(60)
            connection.connect(str(self.state / "run" / "control.sock"))
            connection.sendall(json.dumps(request).encode() + b"\n")
            with connection.makefile() as reader:
                response = json.loads(reader.readline())
        if not response.get("ok"):
            raise RuntimeError(str(response))
        return response

    def cli(self, *args, worker=None):
        env = self.env.copy()
        if worker:
            env["MEWLA_WORKER_ID"] = worker
        result = subprocess.run(
            [self.binary, *args], env=env, text=True, capture_output=True, timeout=60,
        )
        if result.returncode:
            raise RuntimeError(self.name + ": " + result.stderr + result.stdout)
        return json.loads(result.stdout)

    def text(self, worker):
        path = self.state / (worker + ".input")
        return path.read_text() if path.exists() else ""

    def progress(self, worker, status="running"):
        def new_turn():
            tokens = re.findall(r"--turn-id (turn:[^\s]+)", self.text(worker))
            return tokens[-1] if tokens and tokens[-1] != self.turns.get(worker) else None

        turn = wait("new fixture turn", new_turn) if status == "running" else self.turns[worker]
        self.turns[worker] = turn
        self.cli(
            "worker", "progress", "--turn-id", turn, "--status", status,
            "--phase", "reporting" if status == "done" else "working",
            "--attention", "done" if status == "done" else "none",
            "--summary", "inert fixture", "--lease", "300", worker=worker,
        )

    def spawn(self, name):
        repository = str(Path(__file__).resolve().parents[2])
        result = self.cli(
            "worker", "spawn", "--name", name, "--executor", "grok",
            "--cwd", repository, "--prompt", "E2E_READY",
        )
        worker = result["worker"]["id"]
        self.progress(worker)
        return worker, result["brain_work"]["work_id"]

    def send(self, peer, text, worker, reply_to="", kind="request"):
        return self.cli(
            "computers", "send", peer, "--kind", kind, "--reply-to", reply_to,
            "--json", text, worker=worker,
        )["computers"]["message"]["id"]

    def outbox(self):
        return json.loads((self.state / "computers" / "computers.json").read_text())

    def close(self):
        for process in self.processes:
            if process.poll() is None:
                process.send_signal(signal.SIGCONT)
                process.terminate()
        for process in self.processes:
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        subprocess.run(["/usr/bin/tmux", "-S", str(self.socket), "kill-server"], capture_output=True)
        for log in self.logs:
            log.close()


def exercise(root, binary):
    (root / ".zshrc").write_text("")
    (root / "fixture.c").write_text(FIXTURE)
    subprocess.run(["cc", "-o", str(root / "grok"), str(root / "fixture.c")], check=True)
    a, b = Daemon(root, "A", binary), Daemon(root, "B", binary)
    try:
        a.start()
        b.start()
        invite = b.cli("computers", "invite", "--json")["computers"]["invite"]
        # The ordinary authenticated pairing API permits an explicit loopback
        # override; advertised invites correctly exclude loopback addresses.
        a.control({"type": "computers", "computers_request": {
            "action": "add", "invite": invite, "name": "B",
            "urls": ["http://127.0.0.1:" + str(b.port)],
        }})
        aid, work_id = a.spawn("asker")
        wait("paired channel", lambda: a.cli("computers", "list", "--json")["computers"]["computers"][0]["online"])
        wait("B host activation", lambda: any(p.stat().st_size for p in b.state.glob("*.input")))
        request = a.send("B", "E2E_JOB", aid)
        a.progress(aid, "done")
        wait("B Brain request", lambda: any("E2E_JOB" in p.read_text() for p in b.state.glob("*.input")))
        bid, _ = b.spawn("responder")
        peer = b.cli("computers", "list", "--json")["computers"]["computers"][0]["name"]
        question = b.send(peer, "E2E_QUESTION", bid, request, "question")
        b.progress(bid, "done")
        wait("A Worker question", lambda: "E2E_QUESTION" in a.text(aid))
        a.progress(aid)
        answer = a.send("B", "E2E_ANSWER", aid, question, "answer")
        a.progress(aid, "done")
        wait("B Worker answer", lambda: "E2E_ANSWER" in b.text(bid))
        b.progress(bid)

        # Only isolated A is paused. B loses its ACK and crashes with StateSent.
        a.process.send_signal(signal.SIGSTOP)
        reply = b.send(peer, "E2E_FINAL_REPLY", bid, request, "reply")
        wait("B StateSent", lambda: any(x["message"]["id"] == reply and x["state"] == "sent" for x in b.outbox()["outbox"]))
        b.process.kill()
        b.process.wait()
        b.restart()
        a.process.send_signal(signal.SIGCONT)
        wait("A Worker reply", lambda: "E2E_FINAL_REPLY" in a.text(aid))
        delivered = lambda: any(x["message"]["id"] == reply and x["state"] == "delivered" for x in b.outbox()["outbox"])
        wait("restart ACK", delivered)

        # Inject the historical attempt-2 replay into the stopped sender only.
        b.process.terminate()
        b.process.wait(timeout=20)
        state = b.outbox()
        for item in state["outbox"]:
            if item["message"]["id"] == reply:
                item["state"], item["message"]["attempt"] = "sent", 2
        (b.state / "computers" / "computers.json").write_text(json.dumps(state))
        b.restart()
        wait("attempt-2 ACK", delivered)
        assert a.text(aid).count("E2E_QUESTION") == 1
        assert a.text(aid).count("E2E_FINAL_REPLY") == 1
        assert b.text(bid).count("E2E_ANSWER") == 1
        for path in a.state.glob("*.input"):
            if path.stem != aid:
                assert "E2E_FINAL_REPLY" not in path.read_text()
                assert "E2E_QUESTION" not in path.read_text()
        events = a.control({"type": "brain_work_list", "work_id": work_id})["brain_work_events"]
        for marker in ("E2E_QUESTION", "E2E_FINAL_REPLY"):
            copies = [event for event in events if marker in event.get("summary", "")]
            assert len(copies) == 1 and not copies[0]["actionable"], copies
        print(json.dumps({
            "ok": True, "request": request, "question": question, "answer": answer,
            "reply": reply, "a_worker": aid, "b_worker": bid, "work": work_id,
            "a_question_inputs": 1, "a_reply_inputs": 1, "b_answer_inputs": 1,
            "a_brain_reply_inputs": 0, "work_events": 2,
            "restart_with_state_sent": True, "attempt_2_replay": True,
        }), flush=True)
    except Exception:
        for daemon in (a, b):
            for log in daemon.logs:
                print(Path(log.name).read_text()[-3000:], file=sys.stderr)
        raise
    finally:
        a.close()
        b.close()


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("Usage: computer-reply-routing.py /absolute/path/to/mewla")
    with tempfile.TemporaryDirectory(prefix="computer-routing-") as scratch:
        exercise(Path(scratch), Path(sys.argv[1]).resolve())
