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
const codexModels = ["gpt-6.2-astra", "gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1-sol", "gpt-5.6-sol", "gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.5", "gpt-5.5-mini", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.2", "gpt-5.1", "gpt-5", "gpt-image-2"].map(model);
const claudeModels = ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-2"].map(model);
const catalog = {
  revision: 11,
  connections: [
    { id: "conn_a", name: "Team gateway", preset_id: "custom", clients: ["codex"], credential_ready: true, advanced: true, base_url: "https://gw.example.com", credential_hint: "sk-…a1f2", models_fetched_at: new Date().toISOString(), upstream_api: "openai_responses" },
    { id: "conn_b", name: "Team gateway Claude", preset_id: "custom", clients: ["claude"], credential_ready: true, advanced: true, base_url: "https://gw.example.com", credential_hint: "sk-…7c09", models_fetched_at: new Date().toISOString(), upstream_api: "anthropic_messages" },
    { id: "conn_c", name: "Spare relay", preset_id: "custom", clients: ["codex"], credential_ready: false, advanced: true, base_url: "https://relay.example.net/v1", upstream_api: "openai_responses" },
    // A Chat-only endpoint: Codex's requests are translated, and the gateway takes them while it fails.
    { id: "conn_d", name: "Local GLM", preset_id: "custom", clients: ["codex"], credential_ready: true, advanced: true, base_url: "https://glm.example.org/v1", credential_hint: "sk-…3e8d", models_fetched_at: new Date().toISOString(), upstream_api: "openai_chat", fallback_connection_id: "conn_a" },
  ],
  defaults: { codex: { connection_id: "conn_a" }, claude: { connection_id: "conn_b" } },
  // The daemon's presets, as it lists them.
  presets: [{"id":"openai","label":"OpenAI","clients":["codex"]},{"id":"openrouter","label":"OpenRouter","clients":["codex","claude"]},{"id":"anthropic","label":"Anthropic","clients":["claude"]},{"id":"deepseek","label":"DeepSeek","clients":["codex","claude"]},{"id":"kimi","label":"Kimi","clients":["codex","claude"]},{"id":"kimi-cn","label":"Kimi (China)","clients":["codex","claude"]},{"id":"glm","label":"GLM (Z.ai)","clients":["codex","claude"]},{"id":"glm-cn","label":"GLM (China)","clients":["codex","claude"]},{"id":"minimax","label":"MiniMax","clients":["codex","claude"]},{"id":"minimax-cn","label":"MiniMax (China)","clients":["codex","claude"]},{"id":"ollama","label":"Ollama","clients":["codex","claude"],"local":true,"base_url":"http://localhost:11434"},{"id":"lmstudio","label":"LM Studio","clients":["codex","claude"],"local":true,"base_url":"http://localhost:1234"},{"id":"local","label":"Local server","clients":["codex","claude"],"local":true},{"id":"custom","label":"Custom Gateway","clients":["codex","claude"],"advanced":true}],
  models: { conn_a: codexModels, conn_b: claudeModels, conn_c: [], conn_d: ["glm-5", "glm-5-air"].map(model) },
  disabled_models: { conn_a: ["gpt-5.5"] },
  gateway: { running: true, address: "127.0.0.1:3425" },
};
const daemon = new FakeDaemon({
  ...base,
  replies: {
    ...base.replies,
    list_providers: { type: "providers", ...catalog },
    discover_provider_models: { type: "provider_models", connection_id: "conn_a", models: codexModels },
    // A model server running on the computer.
    detect_local_providers: { type: "local_providers", found: [{ preset_id: "ollama", label: "Ollama", base_url: "http://localhost:11434", models: 2, connection_ids: [] }] },
  },
});
installPlatform({ ...platform(), openSocket: () => daemon.open() });
