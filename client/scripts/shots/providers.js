// Model Providers (--entry ui/fixture/ProvidersApp.tsx), design A: Agents (each with its Provider in a chip that
// picks in place) and every Provider once; on a wide pane the shown Provider's page sits beside the agents.
await app.waitFor("text:Docs refresh", { timeout: 20000 });
await app.wait(500);
await app.host.send("links.open", { url: "mewla://settings/model-providers" });
await app.waitFor("id:provider-pick-codex", { timeout: 15000 });
await app.wait(500);
await shot("1-providers");
for (const [w, h] of [[1920, 1080], [2560, 1440]]) {
  await app.setViewport(w, h); await app.setColorScheme("light"); await app.wait(500);
  await app.waitFor("id:providers-split", { timeout: 5000 });
  await snap(`1-providers-${w}`);
  await app.setColorScheme("dark"); await app.wait(300);
  await snap(`1-providers-${w}-dark`);
}
await app.setViewport(390, 844); await app.setColorScheme("light"); await app.wait(400);
await app.press("id:provider-pick-codex");
await app.waitFor("id:provider-pick-codex-conn_a", { timeout: 5000 });
await app.wait(400);
await snap("2-codex-picker-390");
await app.setViewport(1280, 800); await app.wait(500);
await app.press("id:provider-pick-codex");
await app.waitFor("id:provider-pick-codex-conn_a", { timeout: 5000 });
await app.wait(400);
await snap("2-codex-picker-1280");
await app.setViewport(390, 844); await app.wait(400);
await app.host.send("links.open", { url: "mewla://settings/model-providers" });
await app.waitFor("id:provider-conn_a", { timeout: 15000 });
await app.press("id:provider-conn_a");
await app.waitFor("id:provider-detail", { timeout: 5000 });
await app.wait(600);
await snap("3-provider-390");
