// What's New's fixes list (0.4.19's nine), with --entry ui/whatsnew/testing/WhatsNewShots.tsx: on the smallest
// phone (320x568) at its top and scrolled to its end, and at phone 390 at its end; Done pinned under it.
const both = async (name) => {
  await app.setColorScheme("light"); await app.wait(200); await app.screenshot(name + ".light.png");
  await app.setColorScheme("dark"); await app.wait(200); await app.screenshot(name + ".dark.png");
  await app.setColorScheme("light");
};
await app.setViewport(320, 568);
await app.waitFor("id:whats-new-page-0", { timeout: 20000 });
await app.press("id:whats-new-dot-2");
await app.press("id:whats-new-done");
await app.waitUntil(() => !app.shows("id:whats-new"));
await app.press("text:What's new");
await app.waitFor("id:whats-new-fixes");
await app.wait(400);
await both("small-fixes-top");
await app.scroll("id:whats-new-scroll", 4000);
await app.wait(400);
await both("small-fixes-end");
await app.setViewport(390, 844);
await app.wait(400);
await app.scroll("id:whats-new-scroll", 4000);
await app.wait(400);
await both("fixes-end");
