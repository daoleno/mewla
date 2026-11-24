// Resources: what Mewla holds on the computer (GC S2): each kind with its size, oldest item and 7-day growth,
// the Sessions holding the most, what is kept, and what was collected today with Restore.
//   client/scripts/shots.sh scripts/shots/resources-holds.js
await app.waitFor("id:work-card", { timeout: 20000 });
await app.press("id:home-cat");
await app.waitFor("text:Resources", { timeout: 10000 });
await app.press("text:Resources");
await app.waitFor("id:holds", { timeout: 15000 });
await app.scroll("id:holds", 2400);
await app.wait(600);
await shot("resources-holds");
