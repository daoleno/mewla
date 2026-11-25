// A Claude Session picked a model while it works (client/scripts/shots/model-picker.js, with --entry):
// the composer's model chip and the Model sheet show the pick held for the end of the turn, and a
// signed-out Cursor Session offers no model choice at all (owner 11-24: "Could not switch model").
// Never bundled: App.tsx is the app's entry and does not import this.
import "./seed";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";

const base = scenario();
const efforts = ["low", "medium", "high", "xhigh", "max"];
const own = (id: string, name: string) => ({ id, display_name: name, available: true, source: "claude_native", reasoning_efforts: efforts });
const runtime = {
  session_id: "a11y", client: "claude", connection_id: "claude", connection_name: "Claude Code", provider_label: "Claude Code",
  model_id: "haiku", reasoning_effort: "medium", reasoning_efforts: efforts, credential_ready: true, hot_switchable: true,
  pending_model_id: "sonnet", pending_reasoning_effort: "high",
  native_models: [own("default", "Default · Opus 5.5"), own("opus", "Opus 5.5"), own("fable", "Fable 5.2"), own("sonnet", "Sonnet 5.5"), own("haiku", "Haiku 5.5")],
};
const switchable = { structured_events: true, model_profile_managed: true, model_profile_active_switch: true };
const signedOut = { structured_events: true, model_profile_managed: true, model_profile_active_switch: false };
const daemon = new FakeDaemon({
  ...base,
  worker_sessions: (base.worker_sessions ?? []).map((w) =>
    w.id === "a11y" ? { ...w, capabilities: switchable } : w.id === "logs" ? { ...w, capabilities: signedOut } : w),
  replies: {
    ...base.replies,
    get_thread_runtime: { type: "thread_runtime", worker_id: "a11y", runtime },
    set_thread_runtime: { type: "thread_runtime_set", worker_id: "a11y", runtime, persistence_outcome: "applied", persistence_durable: true },
  },
});
installPlatform({ ...platform(), openSocket: () => daemon.open() });
