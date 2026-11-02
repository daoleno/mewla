import { expect, test } from "bun:test";
import type { PluginAccount } from "./connections";
import { cloudProfileGroups, cloudProfileIdentity, cloudProfilesSummary } from "./cloudProfilesModel";
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
  expect(cloudProfileIdentity(aws)).toBe("Production");
  expect(cloudProfileGroups([aws], "789012").signedIn).toHaveLength(1);
  expect(cloudProfileGroups([aws], "production").signedIn).toHaveLength(1);
  const gcp = { ...profile("work", "valid", "gcp"), identity: "user@example.com", project: "my-project" };
  expect(cloudProfileGroups([gcp], "my-project").signedIn).toHaveLength(1);
  expect(cloudProfileIdentity(gcp)).toBe("user@example.com");
});
