import { describe, expect, test } from "nsh/test";
import { desktopArtifact } from "../../core/update/desktopUpdate";
import { appUpdateMenuItem } from "./appUpdateMenu";

const press = () => {};
const update = desktopArtifact("0.4.18");

describe("the cat menu's update row", () => {
  test("nothing while no update waits", () => {
    for (const kind of ["hidden", "idle", "checking", "current", "checkFailed"] as const) expect(appUpdateMenuItem({ kind }, press)).toEqual([]);
  });

  test("its progress while it downloads, Restart to update once it waits, and a way to try again", () => {
    const labels = (state: Parameters<typeof appUpdateMenuItem>[0]) => appUpdateMenuItem(state, press).map((item) => ("label" in item ? item.label : item.type));
    expect(labels({ kind: "available", update })).toEqual(["separator", "Update to 0.4.18"]);
    expect(labels({ kind: "downloading", update, progress: 0.42 })).toEqual(["separator", "Updating… 42%"]);
    expect(labels({ kind: "ready", update, fileName: "" })).toEqual(["separator", "Restart to update to 0.4.18"]);
    let pressed = 0;
    const row = appUpdateMenuItem({ kind: "ready", update, fileName: "" }, () => (pressed += 1))[1] as { disabled?: boolean; onSelect: () => void };
    expect(row.disabled).toBeUndefined();
    row.onSelect();
    expect(pressed).toBe(1);
    expect(labels({ kind: "failed", update, message: "x" })).toEqual(["separator", "Update to 0.4.18 failed: try again"]);
  });
});
