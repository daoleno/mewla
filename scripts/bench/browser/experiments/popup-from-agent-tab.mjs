import { McpClient } from '../mcpclient.mjs';
import fs from 'node:fs';
const T = process.env.TMPDIR + '/bench';
const key = fs.readFileSync(T + '/ext-key', 'utf8').trim();
const c = new McpClient(['node', T + '/mcp/node_modules/@playwright/mcp/cli.js', '--extension', '--executable-path', T + '/chrome-wrap.sh', '--profile-dir-name', 'Default', '--snapshot-mode', 'none', '--codegen', 'none', '--timeout-settle', '100', '--init-page', new URL('../../../../daemon/agentuse/playwright/fast-actions.cjs', import.meta.url).pathname, '--output-dir', T + '/mcp-out'], { ...process.env, DISPLAY: ':97', PLAYWRIGHT_MCP_EXTENSION_TOKEN: key });
await c.start();
await c.call('browser_navigate', { url: 'http://127.0.0.1:8765/form' });
const code = `async (page) => {
  const out = {};
  const t = async (k, f) => { const s = Date.now(); let r; try { r = await f(); } catch (e) { r = 'ERR ' + String(e).slice(0, 100); } out[k] = (out[k] ? out[k] + ',' : '') + (typeof r === 'string' && r.startsWith('ERR') ? r : Date.now() - s); return r; };
  const FRAMES = () => new Promise(r => { let n = 0; const f = () => { n++; requestAnimationFrame(f); }; requestAnimationFrame(f); setTimeout(() => r(n), 1500); });
  out.frames_agent_tab = await page.evaluate(FRAMES);
  // (a) a popup window opened by a real click in the attached tab
  await page.evaluate(() => { const b = document.createElement('button'); b.id = 'pop'; b.textContent = 'pop'; b.onclick = () => window.open('http://127.0.0.1:8765/table', 'agentwin', 'popup,width=900,height=700'); document.body.appendChild(b); });
  const [popup] = await Promise.all([page.context().waitForEvent('page', { timeout: 5000 }).catch(e => 'ERR ' + String(e).slice(0, 80)), page.locator('#pop').click()]);
  if (typeof popup === 'string') { out.popup = popup; return out; }
  out.popup = 'opened';
  out.pages = page.context().pages().length;
  await popup.waitForLoadState('load').catch(() => {});
  out.frames_popup = await popup.evaluate(FRAMES);
  for (let i = 0; i < 4; i++) {
    await t('popup_goto', () => popup.goto('http://127.0.0.1:8765/' + (i % 2 ? 'form' : 'table')));
    await t('popup_screenshot', () => popup.screenshot());
  }
  await t('popup_fill', async () => { await popup.goto('http://127.0.0.1:8765/form'); await popup.locator('#name').fill('x'); });
  out.popup_window = await popup.evaluate(() => JSON.stringify({ w: window.outerWidth, h: window.outerHeight, vis: document.visibilityState }));
  out.agent_tab_still_ok = await page.evaluate(() => 1).catch(e => 'ERR ' + String(e).slice(0, 60));
  await popup.close().catch(() => {});
  return out;
}`;
const r = await c.call('browser_run_code_unsafe', { code });
console.log(r.text.split('\n').filter(l => l.startsWith('{') || /Error/.test(l)).join('\n').slice(0, 1500));
await c.close();
