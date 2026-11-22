// Calendar on its real-shaped scene: the Triggers page, the Trigger that needs you, how it checks.
//   client/scripts/shots.sh --entry "$PWD/ui/fixture/calendar/BusyMorningApp.tsx" scripts/shots/calendar-triggers.js

await app.waitFor("id:work-card", { timeout: 20000 });
await app.press("id:home-cat");
await app.waitFor("text:Resources", { timeout: 10000 });
await app.press("text:Calendar");
await app.waitFor("id:calendar-headline", { timeout: 10000 });
await app.wait(800);
await app.press("id:triggers-open");
await app.waitFor("id:triggers-headline", { timeout: 10000 });
await app.wait(500);
await shot("triggers");
await app.press("text:Home server disk health");
await app.waitFor("id:trigger-ask", { timeout: 10000 });
await app.wait(500);
await shot("trigger");
await app.press("id:trigger-how");
await app.waitFor("id:trigger-check", { timeout: 10000 });
await shot("trigger-check");
