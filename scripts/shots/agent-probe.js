await app.waitFor("id:work-card", { timeout: 20000 });
await app.press("id:brain-menu");
await app.press("id:brain-menu-executor");
await app.waitFor("text:Cursor Agent");
if (app.texts().some((t) => /sign/i.test(t))) throw new Error("an agent row speaks of sign-in: " + app.texts().join(" | "));
await shot("agent-probe");
