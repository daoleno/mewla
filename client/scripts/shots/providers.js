// Model Providers (--entry ui/fixture/ProvidersApp.tsx): Agents (each with what it uses), Providers once,
// an agent's choice, and one Provider's page (key, test, models).
await app.waitFor("text:Docs refresh", { timeout: 20000 });
await app.wait(500);
await app.host.send("links.open", { url: "mewla://settings/model-providers" });
await app.waitFor("id:provider-agent-codex", { timeout: 15000 });
await app.wait(500);
await shot("1-providers");
await app.press("id:provider-agent-codex");
await app.waitFor("id:provider-choose-conn_a", { timeout: 5000 });
await app.wait(400);
await shot("2-codex-uses");
await app.host.send("links.open", { url: "mewla://settings/model-providers" });
await app.waitFor("id:provider-conn_a", { timeout: 15000 });
await app.press("id:provider-conn_a");
await app.waitFor("id:provider-detail", { timeout: 5000 });
await app.wait(600);
await shot("3-provider");
