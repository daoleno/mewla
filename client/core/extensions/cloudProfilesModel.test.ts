import { expect, test } from "bun:test";
import type { PluginAccount } from "./connections";
import { cloudProfileExpiry, cloudProfileGroups, cloudProfileIdentity, cloudProfileName, cloudProfilesSummary } from "./cloudProfilesModel";
const profile = (name: string, status: string, integration = "aws"): PluginAccount => ({ id: name, profile: name, name, profile_status: status, integration, identity: "", enabled: false, status: "authorization_required", history: [] });
test("summaries use sign-in state, independent of agent access", () => {
  expect(cloudProfilesSummary([profile("work", "valid"), profile("personal", "valid")])).toBe("personal · work");
  expect(cloudProfilesSummary([profile("work", "valid"), ...Array.from({ length: 5 }, (_, i) => profile(`old-${i}`, "expired"))])).toBe("1 signed in · 5 expired");
  expect(cloudProfilesSummary([profile("bad", "invalid")])).toBe("1 expired");
});
test("both clouds sort and group; filter appears only above eight", () => {
  for (const service of ["aws", "gcp"]) {
    const accounts = [profile("z", "valid", service), profile("b", "invalid", service), profile("a", "valid", service), profile("u", "unavailable", service)];
    const g = cloudProfileGroups(accounts);
    expect(g.signedIn.map(a => a.profile)).toEqual(["a", "z"]);
    expect(g.expired.map(a => a.profile)).toEqual(["b"]);
    expect(g.unchecked.map(a => a.profile)).toEqual(["u"]);
    expect(g.filterVisible).toBe(false);
    expect(cloudProfileGroups(Array.from({ length: 9 }, (_, i) => profile(`${i}`, "valid"))).filterVisible).toBe(true);
    expect(cloudProfileGroups(accounts, " Z ").signedIn.map(a => a.profile)).toEqual(["z"]);
    expect(accounts[0]!.profile).toBe("z");
  }
});
test("filter includes cloud account identity and project", () => {
  const aws = { ...profile("prod", "valid"), aws: { account_id: "123456789012", account_name: "Production", who: "Root" } };
  expect(cloudProfileIdentity(aws)).toBe("Production · 123456789012");
  expect(cloudProfileGroups([aws], "789012").signedIn).toHaveLength(1);
  expect(cloudProfileGroups([aws], "production").signedIn).toHaveLength(1);
  const gcp = { ...profile("work", "valid", "gcp"), identity: "user@example.com", project: "my-project" };
  expect(cloudProfileGroups([gcp], "my-project").signedIn).toHaveLength(1);
  expect(cloudProfileIdentity(gcp)).toBe("my-project");
  expect(cloudProfileName({ ...gcp, name: "user@example.com", profile: "account:user@example.com" })).toBe("user@example.com");
});

test("sign-in timing is an explicit upper bound and never decides expired state", () => {
  const now = Date.parse("2026-11-02T08:00:00Z");
  const a = profile("work", "valid");
  expect(cloudProfileExpiry(a, now)).toBeNull();
  const timed = { ...a, profile_sign_in_by: "2026-11-02T11:00:00Z", profile_sign_in_limit: "maximum" as const };
  expect(cloudProfileExpiry(timed, now)).toEqual({ label: "Up to 3h left", soon: false });
  expect(cloudProfileExpiry({ ...timed, profile_sign_in_by: "2026-11-02T08:30:00Z" }, now)).toEqual({ label: "Up to 30m left", soon: true });
  expect(cloudProfileExpiry({ ...timed, profile_sign_in_limit: "sso" }, now)?.label).toBe("Sign in in 3h");
  const past = { ...timed, profile_sign_in_by: "2026-10-31T08:00:00Z" };
  expect(cloudProfileExpiry(past, now)).toBeNull();
  expect(cloudProfileGroups([past]).signedIn).toHaveLength(1);
  expect(cloudProfileExpiry({ ...timed, profile_status: "expired" }, now)).toBeNull();
  expect(cloudProfileGroups([{ ...timed, profile_status: "expired" }]).expired).toHaveLength(1);
  // A stale daemon's rotating credential payload cannot reintroduce a countdown.
  expect(cloudProfileExpiry({ ...a, ...{ profile_expires_at: "2026-11-02T08:15:00Z" } }, now)).toBeNull();
});

test("Google Cloud rows summarize accounts without exposing configuration selectors", () => {
  const account = { ...profile("account:person@example.com", "valid", "gcp"), name: "person@example.com", identity: "person@example.com" };
  expect(cloudProfilesSummary([account])).toBe("person@example.com");
  expect(cloudProfileIdentity(account)).toBe("No default project");
});
test("a Google Workspace account is named by its Google account, with no project line", () => {
  const gws = { ...profile("default", "valid", "gws"), name: "person@example.com", identity: "person@example.com" };
  expect(cloudProfileName(gws)).toBe("person@example.com");
  expect(cloudProfileIdentity(gws)).toBe("");
  expect(cloudProfilesSummary([gws])).toBe("person@example.com");
});
