import { expect, test } from "bun:test";
import type { PluginAccount } from "./connections";
import { accountRow, endpointHost, openAccountId } from "./accountListModel";

const account = (patch: Partial<PluginAccount>): PluginAccount => ({ id: "a", integration: "slack", name: "Acme · pat", identity: "Acme · pat", endpoint: "https://slack.com/api", enabled: true, status: "connected", history: [], ...patch } as PluginAccount);

test("every service shows an account as name, what tells it apart, and status", () => {
  expect(accountRow(account({}))).toEqual({ name: "Acme · pat", identity: "", status: { label: "Connected", tone: "success" }, expiry: null });
  expect(accountRow(account({ name: "Work", identity: "Acme · pat" })).identity).toBe("Acme · pat");
  expect(accountRow(account({ enabled: false })).status).toEqual({ label: "Off", tone: "neutral" });
  expect(accountRow(account({ status: "authorization_required" })).status).toEqual({ label: "Needs sign-in again", tone: "warning" });
  expect(accountRow(account({ integration: "mcp", name: "Docs", identity: "", endpoint: "https://mcp.example.com/mcp" })).identity).toBe("mcp.example.com");
  const aws = accountRow(account({ integration: "aws", profile: "prod", profile_status: "valid", aws: { account_id: "123456789012", account_name: "Production", who: "Root" } } as Partial<PluginAccount>));
  expect(aws).toMatchObject({ name: "prod", identity: "Production · 123456789012", status: { label: "Signed in", tone: "success" } });
  expect(accountRow(account({ integration: "hyperliquid", name: "Hyperliquid", identity: "", network: "testnet" } as Partial<PluginAccount>)).identity).toBe("Testnet");
  expect(accountRow(account({ integration: "hyperliquid", name: "Hyperliquid", identity: "", network: "mainnet" } as Partial<PluginAccount>)).identity).toBe("");
  expect(endpointHost("not a url")).toBe("");
});

test("a lone account starts open; several start collapsed; a choice always wins", () => {
  const one = [account({ id: "x" })];
  const two = [account({ id: "x" }), account({ id: "y" })];
  expect(openAccountId(one, undefined)).toBe("x");
  expect(openAccountId(two, undefined)).toBeNull();
  expect(openAccountId(one, null)).toBeNull();
  expect(openAccountId(two, "y")).toBe("y");
});
