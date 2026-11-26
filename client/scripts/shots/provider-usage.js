// A Provider's page (--entry ui/fixture/ProvidersApp.tsx) with what its requests used: the 30-day tiles, its recent
// requests (tokens, cache, cost when priced) and its recent errors.
await app.waitFor("text:Docs refresh", { timeout: 20000 });
await app.host.send("links.open", { url: "mewla://settings/model-providers" });
await app.waitFor("id:provider-conn_a", { timeout: 15000 });
await app.press("id:provider-conn_a");
await app.waitFor("id:provider-usage-totals", { timeout: 5000 });
await app.wait(500);
await shot("usage");
// The whole page on a phone, its recent errors too.
await app.setViewport(390, 1700);
await app.wait(400);
await snap("usage-phone-390");
