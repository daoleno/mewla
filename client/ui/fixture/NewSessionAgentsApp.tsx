// The app on a computer that has some agents and not others, for the New session sheet's shots
// (client/scripts/shots/new-session.js, with --entry): Cursor Agent, Pi and Antigravity are missing, and a launch of one
// is refused as the daemon refuses it. Never bundled: App.tsx is the app's entry and does not import this.
import "./seed";
import { host } from "nsh";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";

const base = scenario();
const executor = (id: string, provider: string, command: string) => ({ id, name: id, provider, command });
const found = (...ids: string[]) => Object.fromEntries(ids.map((id) => [id, { installed: true, signed_in: true }]));
const daemon = new FakeDaemon({
  ...base,
  brain: {
    ...base.brain,
    executors: [
      executor("claude", "claude", "claude"), executor("codex", "codex", "codex"), executor("agent", "cursor", "cursor-agent --force --sandbox disabled"),
      executor("grok", "grok", "grok"), executor("pi", "pi", "pi"), executor("opencode", "opencode", "opencode"),
      executor("agy", "antigravity", "agy"), executor("dsh", "dsh", "dsh"),
    ],
    executor_states: found("claude", "codex", "grok", "opencode", "dsh"),
  },
  missing_agents: { agy: "Antigravity (agy) is not installed on this computer. Install it with: curl -fsSL https://antigravity.google/cli/install.sh | bash" },
});
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
