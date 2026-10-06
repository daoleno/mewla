import { catSVG, zzz } from '../shared/cat.js';

const still = matchMedia('(prefers-reduced-motion: reduce)').matches;

// Mount every cat slot.
for (const slot of document.querySelectorAll('.cat-slot, .cat-stage')) {
  slot.innerHTML = catSVG(slot.dataset.cat || 'sleep');
}
for (const box of document.querySelectorAll('.zzz')) {
  box.innerHTML = zzz({ n: 3, x: 4, y: 30 });
}

const bar = document.querySelector('.bar');
const onScroll = () => bar.classList.toggle('scrolled', scrollY > 8);
addEventListener('scroll', onScroll, { passive: true });
onScroll();

// The day: each beat sets the pose, the caption and the clock hand.
const beats = [...document.querySelectorAll('.beat')];
const stage = document.querySelector('.stage .cat-slot');
const caption = document.querySelector('.stage-caption');
const hand = document.querySelector('.clock .hand');
const zbox = document.querySelector('.stage .zzz');
const ANGLE = { 1: 10, 2: 30, 3: 55, 4: 80, 5: 105 };

function show(beat) {
  const pose = beat.dataset.cat;
  if (stage && stage.dataset.current !== pose) {
    stage.dataset.current = pose;
    stage.innerHTML = catSVG(pose);
    if (!still) stage.firstElementChild.classList.add('cat-swap');
  }
  if (caption) caption.textContent = beat.dataset.caption;
  if (hand) hand.style.setProperty('--angle', `${ANGLE[beat.dataset.step] ?? 0}deg`);
  // z's only while the cat is actually asleep
  if (zbox) zbox.style.opacity = pose === 'sleep' ? '1' : '0';
}

if (beats.length) {
  if (still) {
    beats.forEach((b) => b.classList.add('on'));
    show(beats[beats.length - 1]);
  } else {
    show(beats[0]);
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        e.target.classList.toggle('on', e.isIntersecting);
        if (e.isIntersecting) show(e.target);
      }
    }, { rootMargin: '-45% 0px -45% 0px', threshold: 0 });
    beats.forEach((b) => io.observe(b));
    // keep the first beat lit on a fresh load
    beats[0].classList.add('on');
  }
}

// Reveal below-the-fold blocks once.
const targets = [...document.querySelectorAll('.cards li, .three li, .col-title, .kit .phone, .yours-grid > *, .end-copy > *')];
if (still) {
  targets.forEach((t) => t.classList.add('on'));
} else {
  const armed = [];
  targets.forEach((t, i) => {
    if (t.getBoundingClientRect().top < innerHeight * 0.92) return;
    t.classList.add('rv');
    t.style.animationDelay = `${(i % 4) * 70}ms`;
    armed.push(t);
  });
  const io = new IntersectionObserver((entries, obs) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      e.target.classList.add('on');
      obs.unobserve(e.target);
    }
  }, { rootMargin: '0px 0px -8% 0px' });
  armed.forEach((t) => io.observe(t));
}

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
