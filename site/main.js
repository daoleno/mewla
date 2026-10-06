// Zen landing: the seals, the cat and its toys, and the small bits.
import { buildSeal, startCat } from './sealcat.js';

// Every URL that depends on where Zen's source and builds are published, in one
// place. An empty value hides every link (and the install command) that uses it.
const LINKS = {
  source: 'https://github.com/daoleno/zen',
  releases: 'https://github.com/daoleno/zen/releases',
  apk: 'https://github.com/daoleno/zen/releases/latest',
  installScript: 'https://raw.githubusercontent.com/daoleno/zen/main/install.sh',
};
for (const node of document.querySelectorAll('[data-link]')) {
  const url = LINKS[node.dataset.link];
  node.hidden = !url;
  if (!url) continue;
  if (node.tagName === 'A') node.href = url;
  for (const code of node.querySelectorAll('[data-cmd]')) code.textContent = code.dataset.cmd.replace('{}', url);
}

const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
buildSeal(document.querySelector('[data-seal="mini"]'), { icon: true });
buildSeal(document.querySelector('[data-seal="foot"]'), { icon: true });
const sealSvg = document.querySelector('[data-seal="hero"]');
buildSeal(sealSvg, { ghost: true });
const cat = startCat(sealSvg, { onPlay: (e) => onPlay(e) });
window.zenSeal = cat;

// ---- playtime: pick a toy, keep score, optional sound ------------------------
const MODES = {
  yarn: { name: 'Yarn', unit: ['swat', 'swats'] },
  laser: { name: 'Laser dot', unit: ['catch', 'catches'] },
  feather: { name: 'Feather', unit: ['swat', 'swats'] },
  treats: { name: 'Treats', unit: ['treat', 'treats'] },
};
const toy = document.querySelector('.toy');
const menu = toy.querySelector('.toy-menu'), hud = toy.querySelector('.toy-hud');
const openBtn = toy.querySelector('.toy-btn'), modeBtn = toy.querySelector('.toy-mode');
const hits = hud.querySelector('.toy-score b'), unit = hud.querySelector('.toy-score span');
const streak = hud.querySelector('.toy-streak'), soundBtn = hud.querySelector('.toy-sound');
const sound = makeSound();

function showMenu(open) {
  menu.hidden = !open;
  for (const b of [openBtn, modeBtn]) b.setAttribute('aria-expanded', String(open));
  if (open) (menu.querySelector('[aria-pressed="true"]') ?? menu.querySelector('button')).focus();
}
function bump(node) { node.classList.remove('bump'); void node.offsetWidth; node.classList.add('bump'); }

function onPlay(e) {
  if (e.type === 'start') {
    const m = MODES[e.mode];
    modeBtn.querySelector('use').setAttribute('href', `#ti-${e.mode}`);
    modeBtn.querySelector('.nm').textContent = m.name;
    for (const b of menu.querySelectorAll('[data-mode]')) b.setAttribute('aria-pressed', String(b.dataset.mode === e.mode));
    document.documentElement.classList.toggle('toy-laser', e.mode === 'laser');
  }
  if (e.type === 'stop') {
    hud.hidden = true; openBtn.hidden = false;
    for (const b of menu.querySelectorAll('[data-mode]')) b.setAttribute('aria-pressed', 'false');
    document.documentElement.classList.remove('toy-laser');
    return;
  }
  const m = MODES[e.mode];
  if (hits.textContent !== String(e.hits)) { hits.textContent = e.hits; bump(hits); }
  unit.textContent = m.unit[e.hits === 1 ? 0 : 1];
  streak.hidden = e.streak < 2;
  if (e.streak >= 2) { streak.querySelector('b').textContent = e.streak; bump(streak); }
  sound.play(e.sound ?? e.type);
  if (e.big) sound.play('streak');
}

if (!reduce) {
  toy.hidden = false;
  openBtn.addEventListener('click', () => showMenu(menu.hidden));
  modeBtn.addEventListener('click', () => showMenu(menu.hidden));
  menu.addEventListener('click', (e) => {
    const b = e.target.closest('[data-mode]');
    if (!b) return;
    showMenu(false);
    cat.play(b.dataset.mode);
    hud.hidden = false; openBtn.hidden = true;
    modeBtn.focus();
  });
  hud.querySelector('.toy-close').addEventListener('click', () => { showMenu(false); cat.stop(); openBtn.focus(); });
  soundBtn.addEventListener('click', () => {
    sound.set(!sound.on);
    soundBtn.setAttribute('aria-pressed', String(sound.on));
  });
  addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    if (!menu.hidden) { showMenu(false); (cat.playing ? modeBtn : openBtn).focus(); }
    else if (cat.playing) { cat.stop(); openBtn.focus(); }
  });
  addEventListener('pointerdown', (e) => { if (!menu.hidden && !e.target.closest('.toy')) showMenu(false); }, { passive: true });
}

// Tiny synthesized blips. Off by default; the audio context starts only when a
// visitor turns sound on, so it always follows a gesture.
function makeSound() {
  let ctx = null, on = false;
  function tone(f0, f1, ms, type = 'triangle', gain = 0.1, at = 0) {
    const t = ctx.currentTime + at, o = ctx.createOscillator(), g = ctx.createGain();
    o.type = type;
    o.frequency.setValueAtTime(f0, t);
    o.frequency.exponentialRampToValueAtTime(f1, t + ms / 1000);
    g.gain.setValueAtTime(gain, t);
    g.gain.exponentialRampToValueAtTime(0.0001, t + ms / 1000);
    o.connect(g).connect(ctx.destination);
    o.start(t); o.stop(t + ms / 1000 + 0.02);
  }
  function hiss(ms, gain = 0.06, freq = 1200) {
    const n = Math.ceil(ctx.sampleRate * ms / 1000), buf = ctx.createBuffer(1, n, ctx.sampleRate), d = buf.getChannelData(0);
    for (let i = 0; i < n; i++) d[i] = (Math.random() * 2 - 1) * (1 - i / n);
    const src = ctx.createBufferSource(), f = ctx.createBiquadFilter(), g = ctx.createGain();
    src.buffer = buf; f.type = 'bandpass'; f.frequency.value = freq; g.gain.value = gain;
    src.connect(f).connect(g).connect(ctx.destination);
    src.start();
  }
  const fx = {
    hit: () => tone(700, 320, 90, 'triangle', 0.14),
    miss: () => hiss(180, 0.07, 900),
    pounce: () => { hiss(120, 0.05, 2400); tone(220, 110, 140, 'sine', 0.16, 0.05); },
    catch: () => { tone(523, 523, 90, 'square', 0.05); tone(784, 784, 140, 'square', 0.05, 0.09); },
    chomp: () => { tone(320, 180, 50, 'square', 0.06); tone(300, 160, 50, 'square', 0.06, 0.1); },
    snack: () => fx.chomp(),
    tug: () => hiss(140, 0.06, 3000),
    toss: () => tone(900, 1400, 70, 'sine', 0.05),
    tick: () => tone(1800, 1200, 25, 'sine', 0.03),
    streak: () => [0, 4, 7, 12].forEach((s, i) => tone(523 * 2 ** (s / 12), 523 * 2 ** (s / 12), 110, 'triangle', 0.07, i * 0.07)),
  };
  return {
    get on() { return on; },
    set(v) {
      on = v;
      if (on && !ctx) ctx = new AudioContext();
      if (on) ctx.resume();
    },
    play(name) { if (on && ctx && fx[name]) fx[name](); },
  };
}

// the works-with strip loops: a second, hidden copy follows the first
const row = document.querySelector('.works .row');
const twin = row.cloneNode(true);
twin.setAttribute('aria-hidden', 'true');
row.after(twin);

// illustrations play once, as they come into view
const shown = document.querySelectorAll('.rv');
if ('IntersectionObserver' in window) {
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target); }
  }, { rootMargin: '0px 0px -12% 0px' });
  shown.forEach((n) => io.observe(n));
} else shown.forEach((n) => n.classList.add('in'));

const bar = document.querySelector('.bar');
const onScroll = () => bar.classList.toggle('scrolled', scrollY > 8);
addEventListener('scroll', onScroll, { passive: true });
onScroll();

for (const btn of document.querySelectorAll('[data-copy]')) {
  btn.addEventListener('click', async () => {
    const text = btn.previousElementSibling.textContent.trim();
    try {
      await navigator.clipboard.writeText(text);
      btn.textContent = 'Copied';
    } catch {
      btn.textContent = 'Select';
    }
    setTimeout(() => { btn.textContent = 'Copy'; }, 1600);
  });
}
