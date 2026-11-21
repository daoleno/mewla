// The New session sheet on a computer that has some agents and not others (--entry ui/fixture/NewSessionAgentsApp.tsx):
// the sheet with Cursor, Pi and Antigravity missing, the refusal of Antigravity's launch, and a normal launch of Codex.
// Phone 390, light then dark: agents-missing.light.png ... launch.dark.png.
const both = async (name) => {
  for (const scheme of ["light", "dark"]) {
    await app.setViewport(390, 844);
    await app.setColorScheme(scheme);
    await app.wait(300);
    await app.screenshot(`${name}.${scheme}.png`);
  }
  await app.setColorScheme("light");
};
await app.waitFor("id:work-card", { timeout: 20000 });
await app.host.send("links.open", { url: "mewla://sessions" });
await app.waitFor("id:tray-new", { timeout: 10000 });
await app.press("id:tray-new");
await app.waitFor("id:new-launch", { timeout: 10000 });
await app.wait(500);
await both("1-agents-missing");
await app.press("label:Antigravity, not installed");
await app.press("id:new-launch");
await app.waitFor("id:new-problem", { timeout: 10000 });
await app.wait(300);
await both("2-refused");
await app.press("label:Codex");
await app.press("id:new-launch");
await app.wait(1500);
await both("3-launched");
