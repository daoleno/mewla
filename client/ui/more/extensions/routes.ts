import type { AgentExtensionsScope } from "../../../core/extensions/agentExtensionsModel";

/** Extensions: the services Brain may use, and what each agent installed itself. */
export type ExtensionsPage =
  | { name: "extensions" }
  | { name: "extensions-service"; id: string }
  | { name: "extensions-tools"; service: string; account?: string }
  | { name: "extensions-agent"; scope: AgentExtensionsScope };
