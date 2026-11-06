// Runs in the person's own Chrome, as one browser_run_code_unsafe script, with
// the page of an agent tab. It creates one OAuth client in the Tailscale admin
// console the person is already signed in to, and returns the client's ID and
// secret as MEWLA:<base64 JSON>. It never types a password, and it stops and
// says so when the console asks for one, for a captcha or for a second factor.
//
// CONFIG is filled in by Mewla: { url, name, scopes, dryRun }. With dryRun the
// script goes as far as the button that creates the client and describes what
// it found instead of pressing it.
async (page) => {
  const CONFIG = __CONFIG__;
  const out = (o) => 'MEWLA:' + Buffer.from(JSON.stringify(o)).toString('base64');
  const seen = async () => (await page.locator('button, [role=button], [role=radio], [role=tab], label, th, [role=row]').allInnerTexts().catch(() => []))
    .map((t) => t.replace(/\s+/g, ' ').trim()).filter(Boolean).slice(0, 60);
  const visible = (loc, timeout = 4000) => loc.first().waitFor({ state: 'visible', timeout }).then(() => true, () => false);
  const stop = async (state, step) => out({ state, step, seen: await seen() });

  await page.goto(CONFIG.url, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => {});
  if (!/^https:\/\/login\.tailscale\.com\/admin\//.test(page.url())) return out({ state: 'signed_out' });
  if (await visible(page.getByText(/captcha|verification code|two-factor|security key|verify it.s you/i), 1500)) return out({ state: 'needs_you' });

  // Open the new-credential dialog and choose OAuth.
  const add = page.getByRole('button', { name: /credential/i });
  if (!(await visible(add, 10000))) return stop('unrecognised', 'credential button');
  await add.first().click();
  const kind = page.getByRole('radio', { name: /oauth/i }).or(page.getByRole('button', { name: /^\s*oauth/i })).or(page.getByRole('option', { name: /oauth/i })).or(page.getByText(/^\s*OAuth( client)?\s*$/));
  if (await visible(kind, 5000)) {
    await kind.first().click();
    const next = page.getByRole('button', { name: /^(continue|next)$/i });
    if (await visible(next, 1500)) await next.first().click();
  }

  // Name it, so the person recognises it in their admin console.
  const description = page.getByLabel(/description/i).or(page.getByPlaceholder(/description/i));
  if (await visible(description, 5000)) await description.first().fill(CONFIG.name);

  // Each scope is a row with a Read or Write choice (all:read has only Read).
  const needsTag = [];
  for (const want of CONFIG.scopes.map((s) => ({ ...s, label: new RegExp('^\\s*' + s.label.split(/\s+/).join('[^a-z0-9]*') + '\\s*$', 'i') }))) {
    const row = page.locator('tr, li, [role=row], [role=listitem], div').filter({ has: page.getByText(want.label) }).last();
    if (!(await visible(row, 3000))) return stop('unrecognised', 'scope ' + want.id);
    const level = row.getByRole('radio', { name: want.level === 'write' ? /write/i : /read/i }).or(row.getByRole('checkbox', { name: want.level === 'write' ? /write/i : /read/i })).or(row.getByRole('button', { name: want.level === 'write' ? /^write$/i : /^read$/i }));
    if (!(await visible(level, 2000))) return stop('unrecognised', 'level of ' + want.id);
    await level.first().click();
    if (want.tag) needsTag.push(want.id);
  }

  // Scopes that act on devices need a tag. Pick the first the tailnet has.
  if (needsTag.length) {
    const tags = page.getByRole('combobox', { name: /tag/i }).or(page.getByLabel(/tag/i));
    if (!(await visible(tags, 3000))) return stop('unrecognised', 'tags');
    await tags.first().click();
    const option = page.getByRole('option').first();
    if (!(await visible(option, 3000))) return out({ state: 'needs_tag' });
    await option.click();
    await page.keyboard.press('Escape').catch(() => {});
  }

  const generate = page.getByRole('button', { name: /generate credential/i });
  if (!(await visible(generate, 3000))) return stop('unrecognised', 'generate button');
  if (CONFIG.dryRun) return out({ state: 'dry_run', seen: await seen() });
  await generate.first().click();

  // The secret is shown once. Read it from the page, never from the clipboard.
  await page.getByText(/tskey-client-/).first().waitFor({ state: 'visible', timeout: 15000 }).catch(() => {});
  const text = await page.locator('body').innerText();
  const secret = (text.match(/tskey-client-[A-Za-z0-9]+-[A-Za-z0-9]+/) || [])[0];
  if (!secret) return out({ state: 'created_unread' });
  const id = (secret.match(/^tskey-client-([A-Za-z0-9]+)-/) || [])[1];
  const done = page.getByRole('button', { name: /^done$/i });
  if (await visible(done, 2000)) await done.first().click().catch(() => {});
  return out({ state: 'created', id, secret });
}
