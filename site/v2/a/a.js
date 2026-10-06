import { catSVG, zzz } from '../shared/cat.js';

// Mount the mascot into every slot.
for (const slot of document.querySelectorAll('.cat-slot')) {
  slot.insertAdjacentHTML('afterbegin', catSVG(slot.dataset.cat || 'sleep'));
}
const zz = document.querySelector('.napper .zzz');
if (zz) zz.innerHTML = zzz({ n: 3, x: 66, y: 26 });

// Header border only once the page has moved.
const bar = document.querySelector('.bar');
const onScroll = () => bar.classList.toggle('scrolled', scrollY > 8);
addEventListener('scroll', onScroll, { passive: true });
onScroll();

const still = matchMedia('(prefers-reduced-motion: reduce)').matches;

// Steps light up as they pass the middle of the screen.
const steps = [...document.querySelectorAll('.step')];
if (steps.length && !still) {
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) e.target.classList.toggle('on', e.isIntersecting);
  }, { rootMargin: '-42% 0px -42% 0px' });
  steps.forEach((s) => io.observe(s));
} else {
  steps.forEach((s) => s.classList.add('on'));
}

// Reveal everything below the fold, once.
const targets = [...document.querySelectorAll('.cards li, .three li, .col-title, .kit .phone, .yours-grid > *, .end-copy > *')];
if (still) {
  targets.forEach((t) => t.classList.add('on'));
} else {
  targets.forEach((t, i) => {
    if (t.getBoundingClientRect().top < innerHeight * 0.9) return;
    t.classList.add('rv');
    t.style.animationDelay = `${(i % 4) * 70}ms`;
  });
  const io = new IntersectionObserver((entries, obs) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      e.target.classList.add('on');
      obs.unobserve(e.target);
    }
  }, { rootMargin: '0px 0px -8% 0px' });
  targets.filter((t) => t.classList.contains('rv')).forEach((t) => io.observe(t));
}

// The napper stirs when you scroll, then settles.
const napper = document.querySelector('.napper');
let t;
addEventListener('scroll', () => {
  if (!napper || still) return;
  napper.classList.remove('awake');
  clearTimeout(t);
  t = setTimeout(() => napper.classList.add('awake'), 120);
}, { passive: true });

// Copy buttons.
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
