// Mewla landing: the pet and its toys, the links and the small bits.
import { startPet } from './pet.js';
import { makeSound, startPlay } from './play.js';

// Every URL that depends on where Mewla's source and builds are published, in one
// place. An empty value hides every link (and the install command) that uses it.
const LINKS = {
  source: 'https://github.com/daoleno/mewla',
  releases: 'https://github.com/daoleno/mewla/releases',
  apk: 'https://github.com/daoleno/mewla/releases/latest',
  installScript: 'https://raw.githubusercontent.com/daoleno/mewla/main/install.sh',
};
for (const node of document.querySelectorAll('[data-link]')) {
  const url = LINKS[node.dataset.link];
  node.hidden = !url;
  if (!url) continue;
  if (node.tagName === 'A') node.href = url;
  for (const code of node.querySelectorAll('[data-cmd]')) code.textContent = code.dataset.cmd.replace('{}', url);
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

// ---- playtime: pick a toy, keep score, optional sound ------------------------
const MODES = {
  yarn: { name: 'Yarn', unit: ['swat', 'swats'] },
  laser: { name: 'Laser dot', unit: ['catch', 'catches'] },
  feather: { name: 'Feather', unit: ['swat', 'swats'] },
  treats: { name: 'Treats', unit: ['treat', 'treats'] },
};
const toy = document.querySelector('.toy');
const pet = await startPet(document.querySelector('[data-pet="home"]'), document.querySelector('[data-pet="strip"]'));
const cat = pet && startPlay(pet, (e) => onPlay(e));
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

if (cat) {
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
