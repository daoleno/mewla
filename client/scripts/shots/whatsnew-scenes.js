// What's New's newest scenes, with --entry ui/whatsnew/testing/ScenesShots.tsx: four frames of their loops,
// 0.9 s apart, light then dark (frame-N.light.png, frame-N.dark.png).
await app.setViewport(390, 844);
for (const scheme of ["light", "dark"]) {
  await app.setColorScheme(scheme);
  await app.wait(300);
  for (const n of [0, 1, 2, 3]) { await app.screenshot(`frame-${n}.${scheme}.png`); await app.wait(650); }
}
