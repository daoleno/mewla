// Zen landing: the seals, Brain the cat, and the small bits.
import { buildSeal, startCat } from './sealcat.js';

buildSeal(document.querySelector('[data-seal="mini"]'), { detail: false });
const sealSvg = document.querySelector('[data-seal="hero"]');
buildSeal(sealSvg, { ghost: true });
const brain = startCat(sealSvg);
window.zenSeal = brain;

// one keyboard- and touch-friendly way in: dangle a yarn ball for the cat
const toyBtn = document.querySelector('.toy-btn');
if (!matchMedia('(prefers-reduced-motion: reduce)').matches) {
  toyBtn.hidden = false;
  const label = toyBtn.querySelector('.lbl');
  const set = (on) => {
    brain.setToy(on);
    toyBtn.setAttribute('aria-pressed', String(on));
    label.textContent = on ? 'Stop playing' : 'Play with Brain';
  };
  toyBtn.addEventListener('click', () => set(!brain.playing));
  addEventListener('keydown', (e) => { if (e.key === 'Escape' && brain.playing) set(false); });
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
