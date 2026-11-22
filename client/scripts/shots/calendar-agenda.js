// Calendar on its real-shaped scene: Today, and its end with Earlier open.
//   client/scripts/shots.sh --entry "$PWD/ui/fixture/calendar/BusyMorningApp.tsx" scripts/shots/calendar-agenda.js

await app.waitFor("id:work-card", { timeout: 20000 });
await app.press("id:home-cat");
await app.waitFor("text:Resources", { timeout: 10000 });
await app.press("text:Calendar");
await app.waitFor("id:calendar-headline", { timeout: 10000 });
await app.wait(800);
await shot("today");
await app.press("id:calendar-earlier-row");
await app.scroll("id:agenda", 1400);
await app.wait(500);
await shot("today-end");
