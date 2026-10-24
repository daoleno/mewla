const { describe, expect, it } = require("bun:test");
const { applyTargetSigning, parseAssignments } = require("../set-ios-target-signing");

function fakeProject(names) {
  const targets = names.map((name) => {
    const settings = {};
    return { props: { name }, settings, setBuildSetting: (key, value) => { settings[key] = value; } };
  });
  return { project: { rootObject: { props: { targets } } }, targets };
}

describe("set-ios-target-signing", () => {
  it("gives each target its own profile", () => {
    const { project, targets } = fakeProject(["Mewla", "MewlaLiveWidgets", "Pods-Mewla"]);
    applyTargetSigning(project, { Mewla: "App Profile", MewlaLiveWidgets: "Live Profile" });
    expect(targets[0].settings.PROVISIONING_PROFILE_SPECIFIER).toBe("App Profile");
    expect(targets[1].settings.PROVISIONING_PROFILE_SPECIFIER).toBe("Live Profile");
    expect(targets[2].settings).toEqual({});
    expect(targets[1].settings.CODE_SIGN_STYLE).toBe("Manual");
  });

  it("fails loudly for a missing target or an empty profile name", () => {
    const { project } = fakeProject(["Mewla"]);
    expect(() => applyTargetSigning(project, { MewlaLiveWidgets: "x" })).toThrow("no target named MewlaLiveWidgets");
    expect(() => applyTargetSigning(project, { Mewla: "" })).toThrow("no provisioning profile name");
  });

  it("keeps spaces and equals signs in profile names", () => {
    expect(parseAssignments(["Mewla=Mewla App Store = prod"])).toEqual({ Mewla: "Mewla App Store = prod" });
    expect(() => parseAssignments(["nope"])).toThrow("expected Target=Profile Name");
  });
});
