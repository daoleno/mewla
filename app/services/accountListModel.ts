import type { PluginAccount } from "./connections";
import { accountStatus } from "./connections";
import { cloudProfileExpiry, cloudProfileIdentity, cloudProfileName, cloudProfileStatus } from "./cloudProfilesModel";
import { accountTone, networkLabel } from "./pluginConnectionsModel";

export type AccountRow = {
  name: string;
  /** Second line: what tells two accounts of one service apart. */
  identity: string;
  status: { label: string; tone: "success" | "warning" | "neutral" | "danger" | "accent" };
  expiry: { label: string; soon: boolean } | null;
};

/** A custom service is known by its address; its account has no identity to show. */
export function endpointHost(endpoint?: string): string {
  try { return endpoint ? new URL(endpoint).host : ""; } catch { return ""; }
}

/** One collapsed row of the account list, the same for every service. */
export function accountRow(account: PluginAccount, now = Date.now()): AccountRow {
  const name = cloudProfileName(account);
  if (account.profile || account.integration === "aws" || account.integration === "gcp") {
    return { name, identity: cloudProfileIdentity(account), status: cloudProfileStatus(account), expiry: cloudProfileExpiry(account, now) };
  }
  const custom = account.integration === "mcp" || account.integration === "openapi";
  // A testnet account always says so, so it is never taken for mainnet.
  const network = account.network === "testnet" ? networkLabel(account.network) : null;
  const identity = [network, custom ? endpointHost(account.endpoint) : account.identity && account.identity !== name ? account.identity : ""].filter(Boolean).join(" · ");
  return { name, identity, status: { label: accountStatus(account), tone: accountTone(account) }, expiry: null };
}

/** Which account starts open: the only one, until the person chooses. */
export function openAccountId(accounts: readonly PluginAccount[], chosen: string | null | undefined): string | null {
  if (chosen !== undefined) return chosen;
  return accounts.length === 1 ? accounts[0]!.id : null;
}
