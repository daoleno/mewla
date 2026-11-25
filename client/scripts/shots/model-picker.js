// The model picker (--entry ui/fixture/ModelPickerApp.tsx; owner 11-24, "Could not switch model"): a Claude
// Session whose pick waits for the end of its turn shows it with an hourglass in the chip, the sheet (which says
// "Sonnet 5.5, High effort after this turn"; 11-25) and the Effort list, each model named with its version; a
// signed-out Cursor Session offers no model choice. It fails when either is not so.
await app.waitFor("text:Docs refresh", { timeout: 20000 });
await app.wait(500);
await app.host.send("links.open", { url: "mewla://sessions/a11y" });
await app.waitFor("id:composer", { timeout: 15000 });
await app.type("id:composer", " ");
await app.waitFor("id:model-chip", { timeout: 15000 });
await app.wait(400);
if (!(app.texts().join(" ").includes("Sonnet 5.5"))) throw new Error("the chip does not name the held pick with its version");
await shot("1-chip-held");
await app.press("id:model-chip");
await app.waitFor("id:model-sheet", { timeout: 10000 });
await app.wait(500);
if (!(app.texts().includes("Sonnet 5.5, High effort after this turn"))) throw new Error("the sheet does not say the pick waits for the end of the turn");
await shot("2-sheet-held");
await app.press("id:model-row-sonnet");
await app.waitFor("id:effect-row-high", { timeout: 5000 });
await app.wait(300);
await shot("3-effort-held");
await app.press("id:model-close");
await app.host.send("links.open", { url: "mewla://sessions/logs" });
await app.waitFor("id:composer", { timeout: 15000 });
await app.type("id:composer", " ");
await app.wait(1500);
if (await app.shows("id:model-chip")) throw new Error("a signed-out Cursor Session offers a model choice");
await shot("4-cursor-signed-out");
