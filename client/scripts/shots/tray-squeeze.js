// The home tray's counts and project chips at the narrow widths (--entry ui/fixture/TraySqueezeApp.tsx), the owner's
// capture first (1 needs you, 1 new, 3 running, five projects, one of them long), then every count at once with two
// digits. Per width: the wide pane at its web minimum (1024 window, pane stepped down to 280), the phone tray at 390 and
// 320 (light and dark), and a narrow browser window; each as <count set>-<width>.<scheme>.png.
// It fails when the owner's three counts are all in words in a column too narrow for them: that is the line that wrapped
// onto the chips (owner capture 11-23); the pictures are the proof of the rest.
const sets = { owner: { needs: 1, fresh: 1, running: 3 }, busy: { needs: 12, fresh: 14, running: 17 }, solo: { needs: 0, fresh: 0, running: 3 } };
// One count alone only at the least width: it keeps its words.
const only = { solo: ["phone320"] };
const widths = [["web280", 1024, 800], ["phone390", 390, 844], ["phone320", 320, 700], ["web600", 600, 800]];
const failures = [];
const fresh = async (set) => {
  await app.host.send("squeeze.set", set);
  await app.wait(400);
};
await app.waitFor("id:work-card", { timeout: 20000 });
for (const [name, set] of Object.entries(sets)) {
  for (const [label, w, h] of widths) {
    if (only[name] && !only[name].includes(label)) continue;
    await app.setViewport(w, h);
    if (w < 1024) {
      await app.host.send("links.open", { url: "mewla://sessions" });
      await app.waitFor("id:tray", { timeout: 10000 }).catch(() => {});
    }
    await fresh(set);
    for (const scheme of ["light", "dark"]) {
      await app.setColorScheme(scheme);
      await app.wait(150);
      await app.screenshot(`${name}-${label}.${scheme}.png`);
    }
    await app.setColorScheme("light");
    const counts = app.texts().filter((t) => /(need you|needs you| new|running)$/.test(t) || /^\d+$/.test(t));
    console.log(`${name} ${label}: ${JSON.stringify(counts.slice(0, 8))}`);
    // 390 and the pane's 280 hold two counts in words at most, 320 none of the three.
    const words = ["1 needs you", "12 need you", "1 new", "14 new", "3 running", "17 running"].filter((t) => app.texts().includes(t));
    if (name !== "solo" && ["web280", "phone320"].includes(label) && words.filter((t) => /need|running/.test(t)).length === 2) failures.push(`${name} ${label}: ${words.join(" · ")} in words`);
  }
}

if (failures.length) throw new Error(failures.join("; "));
