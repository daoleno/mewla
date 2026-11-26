// Add Provider (--entry ui/fixture/ProvidersApp.tsx): what is running on the computer first, then the presets for
// the agent; a vendor asks only for its key, a local server only for its address.
await app.waitFor("text:Docs refresh", { timeout: 20000 });
await app.host.send("links.open", { url: "mewla://settings/model-providers" });
await app.waitFor("id:provider-add", { timeout: 15000 });
await app.press("id:provider-add");
await app.waitFor("text:On this computer", { timeout: 5000 });
await app.wait(400);
await shot("presets");
await app.press("id:preset-kimi");
await app.waitFor("id:provider-key", { timeout: 5000 });
await app.wait(300);
await snap("preset-kimi-390");
await app.press("id:page-back").catch(() => undefined);
await app.host.send("links.open", { url: "mewla://settings/model-providers" });
await app.waitFor("id:provider-add", { timeout: 15000 });
await app.press("id:provider-add");
await app.waitFor("text:On this computer", { timeout: 5000 });
await app.press("id:local-ollama-localhost:11434");
await app.waitFor("id:provider-url", { timeout: 5000 });
await app.wait(300);
await snap("local-ollama-390");
