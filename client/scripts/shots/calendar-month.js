// Calendar on its real-shaped scene: Month on a day two ahead, and on a day six back (only what is known).
//   client/scripts/shots.sh --entry "$PWD/ui/fixture/calendar/BusyMorningApp.tsx" scripts/shots/calendar-month.js
const key = (days) => { const d = new Date(); d.setDate(d.getDate() + days); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`; };
await app.waitFor("id:work-card", { timeout: 20000 });
await app.press("id:home-cat");
await app.waitFor("text:Resources", { timeout: 10000 });
await app.press("text:Calendar");
await app.waitFor("id:calendar-headline", { timeout: 10000 });
await app.wait(800);
await app.press("id:calendar-view-month");
await app.waitFor("id:calendar-month", { timeout: 10000 });
await app.press(`id:day-${key(2)}`);
await app.wait(600);
await shot("month-future");
await app.press(`id:day-${key(-6)}`);
await app.wait(600);
await shot("month-past");
