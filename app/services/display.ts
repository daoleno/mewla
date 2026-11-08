import { buildAuthorizationHeader } from "./auth";
import { endpoint } from "./screen";
import type { StoredServer } from "./storage";

/** One agent's private display on a Linux computer: desktop apps it opened, off the person's screen. */
export interface AgentDisplay {
  /** The Worker it belongs to. */
  id: string;
  width: number;
  height: number;
  /** Who holds it now: the person while they hold it, otherwise the agent. */
  control: "agent" | "person";
  /** The person stopped this agent; it stays stopped until they resume it. */
  stopped?: boolean;
  apps: number;
  /** Seconds since the agent last used it; 0 while it acts. */
  idle_s?: number;
}

export interface DisplayList {
  displays: AgentDisplay[];
  /** Whether this computer can run displays at all (Xvnc installed). */
  available: boolean;
  /** What is missing when agents cannot have a screen or a browser on it here; empty when nothing is. */
  reason?: string;
}

export class DisplayRequestError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
    this.name = "DisplayRequestError";
  }
}

async function request(server: StoredServer, method: "GET" | "POST", body?: object, signal?: AbortSignal): Promise<Record<string, unknown>> {
  const url = await endpoint(server, "/display");
  url.protocol = url.protocol === "wss:" ? "https:" : url.protocol === "ws:" ? "http:" : url.protocol;
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "mewla-display" });
  if (signal?.aborted) throw new Error("Display request cancelled");
  const response = await fetch(url.toString(), {
    method, signal,
    headers: { Authorization: authorization, Accept: "application/json", ...(body ? { "Content-Type": "application/json" } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await response.text();
  let result: Record<string, unknown> | undefined;
  try { result = text ? JSON.parse(text) : undefined; } catch { result = undefined; }
  const isJSON = (response.headers.get("content-type") || "").includes("application/json");
  // A server without displays answers 404, or its web UI answers with HTML.
  if (response.status === 404 || response.status === 503 || (response.ok && !isJSON)) {
    throw new DisplayRequestError("This server does not run agent displays.", response.status);
  }
  if (!response.ok || !result) {
    throw new DisplayRequestError(typeof result?.error === "string" ? result.error : "Display request failed", response.status);
  }
  return result;
}

export async function listDisplays(server: StoredServer, signal?: AbortSignal): Promise<DisplayList> {
  const result = await request(server, "GET", undefined, signal);
  return { displays: Array.isArray(result.displays) ? (result.displays as AgentDisplay[]) : [], available: result.available !== false, reason: typeof result.reason === "string" ? result.reason : "" };
}

/** Stop: the agent may not use its display until the person resumes it. */
export async function haltDisplay(server: StoredServer, worker: string, signal?: AbortSignal): Promise<void> {
  await request(server, "POST", { action: "halt", worker }, signal);
}

/** Lifts a Stop: the agent may use its display again. */
export async function resumeDisplay(server: StoredServer, worker: string, signal?: AbortSignal): Promise<void> {
  await request(server, "POST", { action: "resume", worker }, signal);
}
