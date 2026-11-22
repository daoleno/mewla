// An agent that ended while its Work was open (--entry ui/fixture/RelaunchApp.tsx): Brain's thread with the three Work
// cards (restarting, restarted, not restarted), then each Session's own status line. Phone 390, light then dark.
const both = async (name) => {
  for (const scheme of ["light", "dark"]) {
    await app.setViewport(390, 844);
    await app.setColorScheme(scheme);
    await app.wait(300);
    await app.screenshot(`${name}.${scheme}.png`);
  }
  await app.setColorScheme("light");
};
await app.waitFor("text:Gave up after 3 restarts", { timeout: 20000 });
await app.wait(500);
await both("1-cards");
for (const [n, id] of [[2, "r-died"], [3, "r-back"], [4, "r-gave"]]) {
  await app.host.send("links.open", { url: `mewla://sessions/${id}` });
  await app.waitFor("id:session-state", { timeout: 10000 });
  await app.wait(500);
  await both(`${n}-${id}`);
}
