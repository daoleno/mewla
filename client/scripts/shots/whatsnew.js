// What's New (client/ui/whatsnew), with --entry ui/whatsnew/testing/WhatsNewShots.tsx: each page of the newest
// sheet at phone 390 and on the smallest phone (320x568), light and dark. whatsnew-fixes.js: the fixes list.
const both = async (name) => {
  await app.setColorScheme("light"); await app.wait(200); await app.screenshot(name + ".light.png");
  await app.setColorScheme("dark"); await app.wait(200); await app.screenshot(name + ".dark.png");
  await app.setColorScheme("light");
};
await app.setViewport(390, 844);
await app.waitFor("id:whats-new-page-0", { timeout: 20000 });
for (const page of [0, 1, 2]) {
  await app.press("id:whats-new-dot-" + page);
  await app.waitFor("id:whats-new-page-" + page);
  // Two thirds into the loop: what the scene is about is on screen.
  await app.wait(2400);
  await both("page" + page);
}
await app.setViewport(320, 568);
await app.wait(500);
await both("small-pages");
