import { describe, expect, test } from "bun:test";
import { cloudflareIdentity, cloudflareNeedsPoll, cloudflareOutdated, cloudflareRow, cloudflareSecondsLeft, cloudflareStep, type CloudflareStatus } from "./cloudflareCli";

const base: CloudflareStatus = { installed: true, version: "1.0.0-beta.16", pinned: "1.0.0-beta.16", on_path: true, signed_in: false };
const login = { url: "https://dash.cloudflare.com/oauth2/device/verify", code: "V3vsUaLb", expires: "2026-10-14T10:05:00Z" };

describe("cloudflareStep", () => {
  test("walks install, sign-in, code and signed in", () => {
    expect(cloudflareStep(null)).toEqual({ kind: "loading" });
    expect(cloudflareStep({ ...base, installed: false, version: undefined })).toEqual({ kind: "install" });
    expect(cloudflareStep({ ...base, installed: false, problem: "Installing cf needs Node.js 22.18 or later and npm on this server." }).kind).toBe("unavailable");
    expect(cloudflareStep({ ...base, installing: true }).kind).toBe("installing");
    expect(cloudflareStep(base)).toEqual({ kind: "sign_in" });
    expect(cloudflareStep({ ...base, login })).toEqual({ kind: "code", login });
    expect(cloudflareStep({ ...base, signed_in: true })).toEqual({ kind: "signed_in" });
  });
  test("an install in progress wins over everything else", () => {
    expect(cloudflareStep({ ...base, installed: false, installing: true, problem: "x" }).kind).toBe("installing");
  });
});

describe("cloudflareRow", () => {
  test("offers setup until cf is there, then its sign-in state", () => {
    expect(cloudflareRow({ ...base, installed: false }).label).toBeNull();
    expect(cloudflareRow({ ...base, login })).toMatchObject({ label: "Approve", tone: "accent" });
    expect(cloudflareRow(base)).toMatchObject({ label: "Not signed in", subtitle: "cf 1.0.0-beta.16 is installed. Sign in to use it." });
    expect(cloudflareRow({ ...base, signed_in: true, email: "me@example.com" })).toEqual({ subtitle: "me@example.com", label: "Signed in", tone: "success" });
  });
});

describe("cloudflare helpers", () => {
  test("names the sign-in by email, accounts, or the server's token", () => {
    expect(cloudflareIdentity({ ...base, signed_in: true, accounts: [{ id: "a1", name: "Main" }, { id: "a2", name: "" }] })).toBe("Main · a2");
    expect(cloudflareIdentity({ ...base, signed_in: true, token: true, email: "me@example.com" })).toBe("API token on the server");
  });
  test("polls only while the server is working", () => {
    expect(cloudflareNeedsPoll(null)).toBe(false);
    expect(cloudflareNeedsPoll(base)).toBe(false);
    expect(cloudflareNeedsPoll({ ...base, installing: true })).toBe(true);
    expect(cloudflareNeedsPoll({ ...base, login })).toBe(true);
  });
  test("flags a cf other than the pinned release", () => {
    expect(cloudflareOutdated(base)).toBe(false);
    expect(cloudflareOutdated({ ...base, version: "1.0.0-beta.12" })).toBe(true);
    expect(cloudflareOutdated({ ...base, installed: false, version: undefined })).toBe(false);
  });
  test("counts down the code", () => {
    expect(cloudflareSecondsLeft(login, Date.parse("2026-10-14T10:04:00Z"))).toBe(60);
    expect(cloudflareSecondsLeft(login, Date.parse("2026-10-14T10:06:00Z"))).toBe(0);
  });
});
