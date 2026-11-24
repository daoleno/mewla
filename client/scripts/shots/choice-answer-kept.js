// Brain's two-question card answered through Other with typed Chinese text, Submit, and the computer says the
// question passed (--entry ui/fixture/ChoiceGoneApp.tsx; owner 11-24, a Windows user's video). Shots: the review
// step, then what is left of the answer. It fails when the typed answer is not in the message box.
await app.setViewport(1280, 800);
await app.waitFor("id:request-card", { timeout: 20000 });
await app.wait(500);
for (const text of ["把当前策略画成echarts框架的html给我看看", "先展示当前研究进展"]) {
  await app.press("id:choice-other-open");
  await app.type("id:choice-other", text);
  await app.press("id:choice-next");
}
await app.waitFor("id:choice-submit");
await snap("1-review.desktop");
await app.press("id:choice-submit");
await app.waitFor("id:request-sealed", { timeout: 10000 });
await app.wait(400);
await snap("2-after-submit.desktop");
const kept = app.value("id:composer") ?? "";
if (!kept.includes("把当前策略画成echarts框架的html给我看看")) throw new Error(`the typed answer is not in the message box: ${JSON.stringify(kept)}`);
await shot("2-after-submit");
