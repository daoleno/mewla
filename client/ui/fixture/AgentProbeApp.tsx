import "./seed";
import { host } from "nsh";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";

const base = scenario();
const daemon = new FakeDaemon({
  ...base,
  brain: {
    ...base.brain,
    host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me/workspace", provider: "codex", command: "codex" },
    host_executor: { id: "codex", name: "Codex", provider: "codex", command: "codex" },
    executors: [
      { id: "codex", name: "Codex", provider: "codex", command: "codex" },
      { id: "agent", name: "Cursor Agent", provider: "cursor", command: "cursor-agent" },
      { id: "claude", name: "Claude Code", provider: "claude", command: "claude" },
    ],
    executor_states: {
      codex: { installed: true },
      agent: { installed: true },
    },
  },
});
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
