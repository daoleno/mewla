await app.waitFor("id:work-card", { timeout: 20000 });
await app.press("id:brain-menu");
await app.press("id:brain-menu-executor");
await app.waitFor("text:Sign-in not checked");
await shot("agent-probe");
