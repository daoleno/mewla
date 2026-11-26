// A translated Provider's page (--entry ui/fixture/ProvidersApp.tsx): the API it speaks, and the Fallback that takes
// its requests while it fails, with the picker open (only connections the daemon can move them to).
await app.waitFor("text:Docs refresh", { timeout: 20000 });
await app.host.send("links.open", { url: "mewla://settings/model-providers" });
await app.waitFor("id:provider-conn_d", { timeout: 15000 });
await app.press("id:provider-conn_d");
await app.waitFor("id:provider-fallback-pick", { timeout: 5000 });
await app.wait(500);
await shot("fallback");
await app.press("id:provider-fallback-pick");
await app.waitFor("id:provider-fallback-conn_a", { timeout: 5000 });
await app.wait(400);
await snap("fallback-picker-390");
