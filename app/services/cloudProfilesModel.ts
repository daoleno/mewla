import type { PluginAccount } from "./connections";
export function cloudProfileStatus(a: PluginAccount): { label: string; tone: "success" | "warning" | "neutral" } {
  switch (a.profile_status) {
    case "valid": return { label: "Signed in", tone: "success" };
    case "expired": return { label: "Expired", tone: "warning" };
    case "invalid": return { label: "Invalid", tone: "warning" };
    default: return { label: "Unchecked", tone: "neutral" };
  }
}
export function cloudProfileIdentity(a: PluginAccount): string {
  return a.aws?.account_name || a.aws?.account_id || (a.integration === "gcp" ? a.identity || a.project : "") || "";
}
export function cloudProfileGroups(accounts: PluginAccount[], query = "") {
  const needle = query.trim().toLocaleLowerCase();
  const matches = accounts.filter(a => !needle || [a.profile, a.name, a.identity, a.project, a.aws?.account_id, a.aws?.account_name].some(v => v?.toLocaleLowerCase().includes(needle))).sort((a, b) => (a.profile || a.name).localeCompare(b.profile || b.name));
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
  if (accounts.length <= 2 && signedIn.length === accounts.length) return signedIn.map(a => a.profile || a.name).join(" · ");
  return [signedIn.length ? `${signedIn.length} signed in` : "", expired.length ? `${expired.length} expired` : "", unchecked.length ? `${unchecked.length} unchecked` : ""].filter(Boolean).join(" · ");
}
