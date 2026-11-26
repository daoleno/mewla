// Beside (core/web/beside.ts) in a real browser: what a card or a row opens stands beside Brain, drawn with real width.
// Headless `nsh test` passed a beside page laid out to nothing (an <If else> in the beside stack, 11-26), so this
// scene checks the layout under the web target:  client/scripts/shots.sh scripts/shots/beside.js
const wide = (selector, min) => {
  const box = app.layout(selector);
  if (!box || box.width < min) throw new Error(`${selector} is not drawn beside Brain: ${JSON.stringify(box)}`);
};
await app.waitFor("id:work-card", { timeout: 20000 });
// The board fills an ultrawide's pane: its groups as columns side by side, not one narrow column (Brain, 11-26).
await app.setViewport(3440, 1440); await app.wait(800);
await app.waitFor("id:work-board");
const pane = app.layout("id:beside-pane"), columns = app.count("id:work-board-column");
let used = 0;
for (let i = 0; i < columns; i++) used += app.layout(`id:work-board-column >> nth=${i}`).width;
if (columns < 2 || used / pane.width < 0.85) throw new Error(`the board fills ${Math.round(100 * used / pane.width)}% of the pane in ${columns} columns`);
await snap("board-3440");
await app.setViewport(1920, 1080); await app.wait(600);
await app.waitFor("id:work-board");
await app.press("text:Node 22 upgrade >> nth=0"); await app.wait(1200);
wide("id:session-status", 200);
await app.press("id:left-pane >> text:Docs refresh"); await app.wait(1000);
wide("id:work-detail", 600);
await snap("beside-1920");
await app.setViewport(3440, 1440); await app.wait(800);
await app.press("id:left-pane >> text:Cache layer for the catalogue API"); await app.wait(1200);
wide("id:session-chat", 600);
wide("id:terminal-group", 1000);
await snap("beside-3440");
