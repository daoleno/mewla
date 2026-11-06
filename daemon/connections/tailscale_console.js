// Runs in the person's own Chrome, as one browser_run_code_unsafe script, with
// the page of an agent tab. It creates one OAuth client in the Tailscale admin
// console the person is already signed in to, and returns the client's ID and
// secret as MEWLA:<base64 JSON>. It never types a password, and it stops and
// says so when the console asks for one, for a captcha or for a second factor.
//
// CONFIG is filled in by Mewla: { url, name, scopes, dryRun }. With dryRun the
// script goes as far as the button that creates the client and describes what
// it found instead of pressing it.
//
// The page, as it was found: Trust credentials > Credential opens /add, with an
// OAuth / OpenID Connect choice (OAuth preselected), a Description box and
// Continue. Then a form with a preset (Custom scopes / All - Read & Write / All -
// Read) and collapsible sections (General, Devices, Keys, Logging, Settings),
// each row a label with a Read and a Write checkbox, and Generate credential,
// which stays disabled until a scope is chosen.
async (page) => {
  const CONFIG = __CONFIG__;
  const out = (o) => 'MEWLA:' + Buffer.from(JSON.stringify(o)).toString('base64');
  const seen = async () => (await page.locator('button, [role=button], [role=radio], [role=tab], label, h1, h2, th').allInnerTexts().catch(() => []))
    .map((t) => t.replace(/\s+/g, ' ').trim()).filter(Boolean).slice(0, 60);
  const visible = (loc, timeout = 4000) => loc.first().waitFor({ state: 'visible', timeout }).then(() => true, () => false);
  // Where the tab is, without its query: for telling a login page from a changed console.
  const where = () => { try { const u = new URL(page.url()); return u.origin + u.pathname; } catch (e) { return ''; } };
  const stop = async (state, step) => out({ state, step, page: where(), seen: await seen() });

  await page.goto(CONFIG.url, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => {});
  // The console draws after the page loads; its navigation says it is the console.
  await visible(page.getByRole('link', { name: /trust credentials/i }), 12000);
  if (!/^https:\/\/(console|login)\.tailscale\.com\/admin\//.test(page.url())) return out({ state: 'signed_out', page: where(), seen: await seen() });
  if (await visible(page.getByText(/captcha|verification code|two-factor|security key|verify it.s you/i), 1500)) return out({ state: 'needs_you', page: where() });

  // New credential: OAuth, named so the person recognises it, then Continue.
  const add = page.getByRole('button', { name: /credential/i });
  if (!(await visible(add, 10000))) return stop('unrecognised', 'credential button');
  await add.first().click();
  const kind = page.getByRole('radio', { name: /^\s*oauth\b/i });
  if (!(await visible(kind, 8000))) return stop('unrecognised', 'oauth choice');
  await kind.first().check({ force: true }).catch(() => kind.first().click());
  const description = page.getByLabel(/description/i);
  if (await visible(description, 3000)) await description.first().fill(CONFIG.name);
  const next = page.getByRole('button', { name: /^(continue|next)$/i });
  if (!(await visible(next, 3000))) return stop('unrecognised', 'continue button');
  await next.first().click();
  const generate = page.getByRole('button', { name: /generate credential/i });
  if (!(await visible(generate, 15000))) return stop('unrecognised', 'scope form');

  // Each scope is a row of a section; a section that is folded is opened first.
  const rowOf = (s) => page.locator('div, li, tr').filter({ has: page.getByText(s.label, { exact: true }) }).filter({ has: page.locator('input[type=checkbox]') }).last();
  for (const want of CONFIG.scopes) {
    let row = rowOf(want);
    if (!(await visible(row, 800))) {
      const section = page.getByRole('button', { name: new RegExp('^' + want.section + '$') });
      if (await visible(section, 1500)) { await section.first().click(); await page.waitForTimeout(400); }
      row = rowOf(want);
      if (!(await visible(row, 2500))) return stop('unrecognised', 'scope ' + want.section + ' ' + want.label);
    }
    // Each box is a button with role checkbox next to a hidden input; the button is the control.
    const name = want.level === 'write' ? /^write$/i : /^read$/i;
    const control = row.getByRole('checkbox', { name }).or(row.getByRole('button', { name })).first();
    const hidden = row.locator('input[type=checkbox]').nth(want.level === 'write' ? 1 : 0);
    const on = () => control.isChecked().catch(() => hidden.isChecked().catch(() => false));
    if (!(await on())) {
      if (await visible(control, 1500)) await control.click();
      await page.waitForTimeout(150);
    }
    if (!(await on())) return stop('unrecognised', 'level of ' + want.label);
  }

  // Scopes on devices and keys ask for a tag each ("Add tags" fields). Each takes
  // the first tag the tailnet offers: from the open list, or from what typing
  // "tag:" suggests. When none is offered, the page's own words are returned.
  await page.waitForTimeout(500);
  const popup = async () => (await page.locator('[role=listbox], [role=menu], [role=dialog], [data-radix-popper-content-wrapper], ul').allInnerTexts().catch(() => []))
    .map((t) => t.replace(/\s+/g, ' ').trim()).filter(Boolean).slice(0, 8);
  // A chosen tag turns its field into a chip, so the fields are looked up again each round.
  for (let round = 0; round < 4; round++) {
    const field = page.getByRole('combobox', { name: /add tags?/i }).first();
    if (!(await visible(field, round ? 1000 : 2000))) break;
    await field.click();
    let option = page.getByRole('option').first();
    if (!(await visible(option, 1500))) {
      await field.pressSequentially('tag:', { delay: 60 }).catch(() => {});
      option = page.getByRole('option').first();
    }
    if (!(await visible(option, 2500))) return out({ state: 'needs_tag', page: where(), seen: await popup() });
    await option.click();
    await page.keyboard.press('Escape').catch(() => {});
    await page.waitForTimeout(400);
  }
  if (await generate.first().isDisabled().catch(() => false)) return stop('needs_tag', 'generate disabled');
  if (CONFIG.dryRun) return out({ state: 'dry_run', seen: await seen() });
  await generate.first().click();

  // The secret is shown once, as text or in a field. Read it from the page, never the clipboard.
  const find = () => page.evaluate(() => {
    const m = (s) => (String(s || '').match(/tskey-client-[A-Za-z0-9]+-[A-Za-z0-9]+/) || [])[0];
    for (const e of document.querySelectorAll('input, textarea')) { const v = m(e.value); if (v) return v; }
    return m(document.body.innerText);
  });
  let secret;
  for (let i = 0; i < 30 && !secret; i++) { secret = await find().catch(() => undefined); if (!secret) await page.waitForTimeout(500); }
  if (!secret) return out({ state: 'created_unread' });
  const id = (secret.match(/^tskey-client-([A-Za-z0-9]+)-/) || [])[1];
  const done = page.getByRole('button', { name: /^done$/i });
  if (await visible(done, 2000)) await done.first().click().catch(() => {});
  return out({ state: 'created', id, secret });
}
