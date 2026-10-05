import { buildAuthorizationHeader } from "./auth";
import type { StoredServer } from "./storage";
import type { PendingEnrollment } from "./enrollmentApproval";

export function decodePendingEnrollment(raw: Record<string, unknown>): PendingEnrollment | null {
  const id = raw.request_id ?? raw.id;
  const number = raw.verification_number ?? raw.number;
  if (typeof id !== "string" || typeof number !== "string" || !/^\d{3}$/.test(number) || typeof raw.expires_at !== "string") return null;
  return { id, verificationNumber: number, deviceName: String(raw.device_name || "New device"), platform: String(raw.platform || "unknown"), origin: String(raw.origin || "Unknown address"), expiresAt: raw.expires_at };
}

async function enrollmentFetch(server: StoredServer, path: string, body?: object) {
  const url = new URL(server.url);
  url.protocol = url.protocol === "wss:" ? "https:" : "http:";
  url.pathname = `/enrollment/${path}`;
  url.search = "";
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId || "", purpose: "zen-enrollment:decision:POST:/enrollment/decision" });
  return fetch(url.toString(), { method: body ? "POST" : "GET", headers: { Authorization: authorization, "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
}
export async function pendingEnrollments(server: StoredServer): Promise<PendingEnrollment[]> {
  const response = await enrollmentFetch(server, "pending");
  if (!response.ok) throw new Error("Could not load pending requests.");
  const raw = await response.json();
  return (raw.requests || []).filter((r: Record<string, unknown>) => r.status === "pending").map(decodePendingEnrollment).filter(Boolean);
}
export async function decideEnrollment(server: StoredServer, request: PendingEnrollment, number: string, approve: boolean): Promise<void> {
  const response = await enrollmentFetch(server, "decision", { request_id: request.id, verification_number: number, approve });
  if (!response.ok) {
    const detail = await response.text();
    if (response.status === 409 && detail.includes("number")) throw new Error("That number doesn’t match. Check the new device and try again.");
    throw new Error(response.status === 409 ? "This request is no longer pending." : "Could not send the decision. Please try again.");
  }
}
