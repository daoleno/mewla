// Calendar's Today on a scene entry (ui/fixture/calendar/*App.tsx):
//   client/scripts/shots.sh --entry "$PWD/ui/fixture/calendar/BusyNightApp.tsx" scripts/shots/calendar-today.js
await app.waitFor("id:work-card", { timeout: 20000 });
await app.press("id:home-cat");
await app.waitFor("text:Resources", { timeout: 10000 });
await app.press("text:Calendar");
await app.waitFor("id:calendar-headline", { timeout: 10000 });
await app.wait(800);
await shot("today");
