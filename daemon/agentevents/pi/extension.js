// Mewla's Pi extension. Mewla loads it into each Pi it starts (pi -e) so the
// daemon learns what Pi is doing and can hand it a message or a stop, which
// the Pi terminal UI has no other way to receive. It talks JSON lines over
// the daemon's local socket and sends no prompt text: only a digest of it.
const net = require("net");
const crypto = require("crypto");

const SOCKET = __MEWLA_SOCKET__;
const RETRY_MS = 2000;

module.exports = function (pi) {
  let sock = null;
  let ctx = null;
  let reason = "startup";
  let stopped = false;
  let seq = 0;
  let turn = "";
  let lastEnd = null;
  let retryTimer = null;

  const send = (message) => {
    try {
      if (sock && sock.writable) sock.write(JSON.stringify(message) + "\n");
    } catch (_) {}
  };

  const hello = () => {
    const manager = ctx.sessionManager;
    send({
      t: "hello",
      reason,
      session: manager.getSessionFile() || "",
      id: manager.getSessionId(),
      running: !ctx.isIdle(),
      turn,
    });
  };

  const handle = (line) => {
    let message;
    try {
      message = JSON.parse(line);
    } catch (_) {
      return;
    }
    if (message.t === "send" && ctx) {
      try {
        const options = ctx.isIdle() ? undefined : { deliverAs: "steer" };
        pi.sendUserMessage(message.text, options);
        send({ t: "sent", id: message.id });
      } catch (error) {
        send({ t: "send_failed", id: message.id, error: String(error && error.message || error) });
      }
    } else if (message.t === "abort" && ctx && !ctx.isIdle()) {
      ctx.abort();
    }
  };

  const connect = () => {
    if (stopped || !ctx) return;
    const socket = net.connect(SOCKET);
    sock = socket;
    let buffer = "";
    socket.on("connect", hello);
    socket.on("data", (chunk) => {
      buffer += chunk;
      let end;
      while ((end = buffer.indexOf("\n")) >= 0) {
        handle(buffer.slice(0, end));
        buffer = buffer.slice(end + 1);
      }
    });
    socket.on("error", () => {});
    socket.on("close", () => {
      if (sock === socket) sock = null;
      if (!stopped) {
        retryTimer = setTimeout(connect, RETRY_MS);
        if (retryTimer.unref) retryTimer.unref();
      }
    });
  };

  pi.on("session_start", async (event, context) => {
    ctx = context;
    reason = event.reason;
    stopped = false;
    if (sock) {
      hello();
    } else {
      connect();
    }
  });

  pi.on("input", async (event, context) => {
    ctx = context;
    send({
      t: "input",
      source: event.source,
      sha: crypto.createHash("sha256").update(event.text || "").digest("hex"),
    });
  });

  pi.on("agent_start", async (event, context) => {
    ctx = context;
    turn = context.sessionManager.getSessionId() + ":" + ++seq;
    lastEnd = null;
    send({ t: "turn_start", turn });
  });

  for (const name of ["tool_execution_start", "turn_end"]) {
    pi.on(name, async () => send({ t: "progress", turn }));
  }

  pi.on("agent_end", async (event) => {
    const last = event.messages[event.messages.length - 1];
    lastEnd = last && last.role === "assistant" ? { stop: last.stopReason, error: last.errorMessage } : null;
  });

  pi.on("agent_settled", async (event) => {
    send({
      t: "turn_end",
      turn,
      aborted: !!event.aborted,
      stop: lastEnd ? lastEnd.stop : "",
      error: lastEnd && lastEnd.error ? lastEnd.error : "",
    });
  });

  pi.on("session_shutdown", async (event) => {
    if (event.reason === "quit") {
      send({ t: "bye" });
      stopped = true;
      if (retryTimer) clearTimeout(retryTimer);
      if (sock) sock.end();
    }
  });
};
