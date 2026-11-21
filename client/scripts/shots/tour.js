// The default tour: Brain's thread, the computer page, the tray, Calendar. Steps are plain `nsh test` stdin steps.
await app.waitFor("id:work-card", { timeout: 20000 });
await shot("brain");
await app.press("id:home-cat");
await app.waitFor("text:Resources", { timeout: 10000 });
await shot("computer");
await app.press("text:Calendar");
await app.wait(800);
await shot("calendar");
