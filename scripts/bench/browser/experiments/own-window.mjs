// Frames and navigation cost in a tab of a separate window, via raw CDP.
const ver = await (await fetch('http://127.0.0.1:9333/json/version')).json();
const ws = new WebSocket(ver.webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r));
let id = 0; const pending = new Map();
ws.addEventListener('message', e => { const m = JSON.parse(e.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); } });
const send = (method, params = {}, sessionId) => new Promise(res => { const i = ++id; pending.set(i, res); ws.send(JSON.stringify({ id: i, method, params, sessionId })); });
const frames = `new Promise(r=>{let n=0;const s=performance.now();const f=()=>{n++;if(performance.now()-s>1500)r(n);else requestAnimationFrame(f)};requestAnimationFrame(f)})`;
for (const opts of [{ newWindow: true, background: true }, { newWindow: true }]) {
  const { result: { targetId } } = await send('Target.createTarget', { url: 'http://127.0.0.1:8765/form', ...opts });
  const { result: { sessionId } } = await send('Target.attachToTarget', { targetId, flatten: true });
  await new Promise(r => setTimeout(r, 800));
  const ev = await send('Runtime.evaluate', { expression: frames, awaitPromise: true }, sessionId);
  const navs = [];
  for (let i = 0; i < 4; i++) {
    const t = performance.now();
    await send('Page.navigate', { url: 'http://127.0.0.1:8765/' + (i % 2 ? 'form' : 'table') }, sessionId);
    await send('Runtime.evaluate', { expression: 'document.readyState', awaitPromise: true }, sessionId);
    navs.push(Math.round(performance.now() - t));
  }
  const t = performance.now(); await send('Page.captureScreenshot', { format: 'png' }, sessionId); const shot = Math.round(performance.now() - t);
  console.log(JSON.stringify(opts), 'frames/1.5s', ev.result?.result?.value, 'nav ms', navs.join(','), 'screenshot ms', shot);
  await send('Target.closeTarget', { targetId });
}
ws.close();
