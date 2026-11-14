// mewla.ai: the install tabs, copy buttons and the bar. The screens are
// screenshots of the app; each rises in once as it comes into view.

const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

// ---- the bar, copy buttons, the install tabs --------------------------------
// Where the install script lives, in one place; the app's first-run copy is
// checked against this page (client/core/onboarding.test.ts).
const LINKS = { installScript: 'https://mewla.ai/install.sh' };
for (const node of $$('[data-link]')) {
  for (const code of $$('[data-cmd]', node)) code.textContent = code.dataset.cmd.replace('{}', LINKS[node.dataset.link]);
}
const bar = $('.bar');
const onScroll = () => bar.classList.toggle('scrolled', scrollY > 8);
addEventListener('scroll', onScroll, { passive: true });
onScroll();

// A one-line command wider than its card scrolls; a fade at the right edge
// shows there is more until it is scrolled to the end.
for (const code of $$('.cmd-card code')) {
  const edge = () => code.classList.toggle('more', code.scrollLeft + code.clientWidth < code.scrollWidth - 2);
  code.addEventListener('scroll', edge, { passive: true });
  if (window.ResizeObserver) new ResizeObserver(edge).observe(code);
  else window.addEventListener('resize', edge);
  edge();
}

for (const btn of $$('[data-copy]')) {
  btn.addEventListener('click', async () => {
    const text = btn.previousElementSibling.textContent.trim();
    try { await navigator.clipboard.writeText(text); } catch { return; }
    btn.classList.add('ok');
    setTimeout(() => btn.classList.remove('ok'), 1400);
  });
}

function tabs(buttons, key, pick) {
  for (const b of buttons) {
    b.addEventListener('click', () => {
      for (const o of buttons) {
        const on = o === b;
        o.classList.toggle('on', on);
        o.setAttribute(o.hasAttribute('aria-selected') ? 'aria-selected' : 'aria-pressed', String(on));
      }
      pick(b.dataset[key]);
    });
  }
}
tabs($$('[data-os]'), 'os', (os) => {
  for (const c of $$('[data-os-cmd]')) c.hidden = c.dataset.osCmd !== os;
  for (const c of $$('[data-os-note]')) c.hidden = c.dataset.osNote !== os;
});
if (/Windows/.test(navigator.userAgent)) $('[data-os="win"]')?.click();

const rises = $$('.rise');
if (reduce || !('IntersectionObserver' in window)) {
  for (const el of rises) el.classList.add('seen');
} else {
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      e.target.classList.add('seen');
      io.unobserve(e.target);
    }
  }, { rootMargin: '0px 0px -10% 0px' });
  for (const el of rises) io.observe(el);
}
