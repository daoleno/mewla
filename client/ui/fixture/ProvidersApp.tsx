// Model Providers (client/scripts/shots/providers.js, with --entry): two agents, each on a Provider, and the
// Providers list once, shaped like a real computer's catalog (counts only; names are generic).
// Never bundled: App.tsx is the app's entry and does not import this.
import "./seed";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";

const base = scenario();
const model = (id: string) => ({ id, available: true, source: "discovered" });
const codexModels = ["gpt-6.2-astra", "gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1-sol", "gpt-5.6-sol", "gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.5", "gpt-image-2"].map(model);
const claudeModels = ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-2"].map(model);
const catalog = {
  revision: 11,
  connections: [
    { id: "conn_a", name: "Team gateway", preset_id: "custom", clients: ["codex"], credential_ready: true, advanced: true, base_url: "https://gw.example.com", credential_hint: "sk-…a1f2", models_fetched_at: new Date().toISOString() },
    { id: "conn_b", name: "Team gateway Claude", preset_id: "custom", clients: ["claude"], credential_ready: true, advanced: true, base_url: "https://gw.example.com", credential_hint: "sk-…7c09", models_fetched_at: new Date().toISOString() },
    { id: "conn_c", name: "Spare relay", preset_id: "custom", clients: ["codex"], credential_ready: false, advanced: true, base_url: "https://relay.example.net/v1" },
  ],
  defaults: { codex: { connection_id: "conn_a" }, claude: { connection_id: "conn_b" } },
  presets: [],
  models: { conn_a: codexModels, conn_b: claudeModels, conn_c: [] },
  disabled_models: { conn_a: ["gpt-5.5"] },
  gateway: { running: true, address: "127.0.0.1:3425" },
};
const daemon = new FakeDaemon({
  ...base,
  replies: {
    ...base.replies,
    list_providers: { type: "providers", ...catalog },
    discover_provider_models: { type: "provider_models", connection_id: "conn_a", models: codexModels },
  },
});
installPlatform({ ...platform(), openSocket: () => daemon.open() });
