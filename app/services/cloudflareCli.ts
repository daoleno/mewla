/**
 * Cloudflare on Extensions is the server machine's cf CLI, not a plugin
 * account: Mewla installs it and signs it in, and every Brain and Worker
 * shell on that machine then uses it directly. Nothing here gates a single
 * cf command, so the page says so instead of showing tool switches.
 */
export type CloudflareAccount = { id: string; name: string };
export type CloudflareLogin = { url: string; code: string; expires: string };
export type CloudflareStatus = {
  installed: boolean;
  version?: string;
  /** The cf release Mewla installs. */
  pinned: string;
  path?: string;
  /** Agents' shells find this cf by name. */
  on_path: boolean;
  installing?: boolean;
  /** What stops installing, such as a missing Node.js. */
  problem?: string;
  signed_in: boolean;
  email?: string;
  accounts?: CloudflareAccount[];
  /** Signed in by CLOUDFLARE_API_TOKEN in the server's environment, which Mewla can't sign out. */
  token?: boolean;
  login?: CloudflareLogin;
  message?: string;
};
export type CloudflareAction = "cloudflare_status" | "cloudflare_install" | "cloudflare_login" | "cloudflare_cancel" | "cloudflare_logout";

/** The one thing the Cloudflare page offers next. */
export type CloudflareStep =
  | { kind: "loading" }
  | { kind: "unavailable"; detail: string }
  | { kind: "install" }
  | { kind: "installing" }
  | { kind: "sign_in" }
  | { kind: "code"; login: CloudflareLogin }
  | { kind: "signed_in" };

export function cloudflareStep(status: CloudflareStatus | null): CloudflareStep {
  if (!status) return { kind: "loading" };
  if (status.installing) return { kind: "installing" };
  if (!status.installed) return status.problem ? { kind: "unavailable", detail: status.problem } : { kind: "install" };
  if (status.login) return { kind: "code", login: status.login };
  return status.signed_in ? { kind: "signed_in" } : { kind: "sign_in" };
}

export type CloudflareTone = "success" | "warning" | "neutral" | "accent";

/** The Extensions row: its subtitle and status, or a Set up button when nothing is there yet. */
export function cloudflareRow(status: CloudflareStatus | null): { subtitle: string; label: string | null; tone: CloudflareTone } {
  const step = cloudflareStep(status);
  switch (step.kind) {
    case "loading": return { subtitle: CLOUDFLARE_DESCRIPTION, label: null, tone: "neutral" };
    case "unavailable": return { subtitle: step.detail, label: "Needs Node.js", tone: "warning" };
    case "install": return { subtitle: CLOUDFLARE_DESCRIPTION, label: null, tone: "neutral" };
    case "installing": return { subtitle: "Installing cf…", label: "Installing", tone: "accent" };
    case "code": return { subtitle: "Waiting for approval on Cloudflare", label: "Approve", tone: "accent" };
    case "sign_in": return { subtitle: `${status!.version ? `cf ${status!.version}` : "cf"} is installed. Sign in to use it.`, label: "Not signed in", tone: "neutral" };
    case "signed_in": return { subtitle: cloudflareIdentity(status!), label: "Signed in", tone: "success" };
  }
}

export const CLOUDFLARE_DESCRIPTION = "The cf CLI for Brain and Workers: Workers, R2, D1, KV, DNS and the rest of the Cloudflare API";

/** Who cf is signed in as: the email, else the accounts it can reach. */
export function cloudflareIdentity(status: CloudflareStatus): string {
  if (status.token) return "API token on the server";
  const names = (status.accounts ?? []).map((account) => account.name || account.id).filter(Boolean);
  return status.email || names.join(" · ") || "Cloudflare account";
}

/** Install and sign-in finish on the server; the page asks again until they do. */
export function cloudflareNeedsPoll(status: CloudflareStatus | null): boolean {
  return !!status && (!!status.installing || !!status.login);
}

/** A cf other than the release Mewla installs: offer to install that one. */
export function cloudflareOutdated(status: CloudflareStatus | null): boolean {
  return !!status?.installed && !!status.version && status.version !== status.pinned;
}

/** Seconds left to approve a code, never negative. */
export function cloudflareSecondsLeft(login: CloudflareLogin, now = Date.now()): number {
  return Math.max(0, Math.round((Date.parse(login.expires) - now) / 1000));
}
