import type { PluginAccount } from "./connections";
export function cloudProfileName(a: PluginAccount): string { return a.integration === "gcp" ? a.name || a.identity : a.profile || a.name; }

export function cloudProfileExpiry(a: PluginAccount, now = Date.now()): { label: string; soon: boolean } | null {
  const at = Date.parse(a.profile_sign_in_by || "");
  if (!Number.isFinite(at) || !a.profile_sign_in_limit || a.profile_status !== "valid") return null;
  const delta = at - now;
  // A clock cannot establish authentication failure. Let the CLI refresh and
  // identity check determine whether this profile needs sign-in again.
  if (delta <= 0) return null;
  const minutes = Math.max(1, Math.ceil(delta / 60000));
  const value = minutes >= 1440 ? `${Math.floor(minutes / 1440)}d` : minutes >= 60 ? `${Math.floor(minutes / 60)}h` : `${minutes}m`;
  return { label: a.profile_sign_in_limit === "maximum" ? `Up to ${value} left` : `Sign in in ${value}`, soon: delta <= 3600000 };
}

export function cloudProfileStatus(a: PluginAccount): { label: string; tone: "success" | "warning" | "neutral" } {
  if (a.checking) return { label: "Checking…", tone: "neutral" };
  switch (a.profile_status) {
    case "valid": return { label: "Signed in", tone: "success" };
    case "expired": return { label: "Expired", tone: "warning" };
    case "invalid": return { label: "Invalid", tone: "warning" };
    default: return { label: "Unchecked", tone: "neutral" };
  }
}
export function cloudProfileIdentity(a: PluginAccount): string {
  return a.integration === "gcp" ? a.project || "No default project" : [a.aws?.account_name, a.aws?.account_id].filter(Boolean).join(" · ");
}
export function cloudProfileGroups(accounts: PluginAccount[], query = "") {
  const needle = query.trim().toLocaleLowerCase();
  const matches = accounts.filter(a => !needle || [a.profile, a.name, a.identity, a.project, a.aws?.account_id, a.aws?.account_name].some(v => v?.toLocaleLowerCase().includes(needle))).sort((a, b) => cloudProfileName(a).localeCompare(cloudProfileName(b)));
  return {
    signedIn: matches.filter(a => a.profile_status === "valid"),
    unchecked: matches.filter(a => !["valid", "expired", "invalid"].includes(a.profile_status || "")),
    expired: matches.filter(a => ["expired", "invalid"].includes(a.profile_status || "")),
    filterVisible: accounts.length > 8,
  };
}
export function cloudProfilesSummary(accounts: PluginAccount[]): string {
  if (!accounts.length) return "";
  const { signedIn, expired, unchecked } = cloudProfileGroups(accounts);
  if (accounts.length <= 2 && signedIn.length === accounts.length) return signedIn.map(cloudProfileName).join(" · ");
  return [signedIn.length ? `${signedIn.length} signed in` : "", expired.length ? `${expired.length} expired` : "", unchecked.length ? `${unchecked.length} unchecked` : ""].filter(Boolean).join(" · ");
}
