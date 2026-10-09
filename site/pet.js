// The landing's pet: the default pet's clips from pets/ (written by
// scripts/import-pets.py, the same pack the app plays). It sleeps in the hero
// seal, wakes to watch its team orbit, and when you scroll it leaves the seal
// empty and follows the story: it walks to the card nearest the upper middle
// of the screen and acts it out (data-act).
const ROOT = 'pets/';

// What each story card asks of it, as one of the pack's Brain-state clips.
const ACTS = { wake: 'delegating', stamp: 'delivered', patrol: 'working', perk: 'attention', sleep: 'idle' };

export async function startPet(home, strip) {
  const index = await (await fetch(`${ROOT}index.json`)).json();
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const src = (clip) => `${ROOT}${index.default}/${clip}.webp`;
  fillStrip(strip, index, reduce);
  if (reduce) {
    home.src = `${ROOT}${index.default}/portrait.webp`;
    return;
  }
  for (const clip of Object.keys(index.clips)) new Image().src = src(clip);

  // One-shots (waking, going_back, delivered) restart from their first frame
  // only on a fresh URL, so each play gets its own fragment.
  let plays = 0;
  const play = (img, clip) => { img.src = `${src(clip)}#${++plays}`; };
  const after = (clip, fn) => setTimeout(fn, index.clips[clip].durationMs);

  // ---- home: the hero seal --------------------------------------------------
  let away = false, homeTimer = 0;
  const homeTo = (steps) => {
    clearTimeout(homeTimer);
    const [clip, ...rest] = steps;
    play(home, clip);
    if (rest.length) homeTimer = after(clip, () => homeTo(rest));
  };
  homeTimer = setTimeout(() => homeTo(['waking', 'delegating']), 2200);
  home.addEventListener('click', () => { if (!away) homeTo(['delivered', 'delegating']); });

  // ---- away: the pet that walks the story -----------------------------------
  const pet = document.createElement('img');
  pet.className = 'pet';
  pet.alt = '';
  pet.setAttribute('aria-hidden', 'true');
  document.body.append(pet);
  let perch = null, at = null, face = 1, actTimer = 0, arriveTimer = 0;

  function spot(node) {
    const r = node.getBoundingClientRect();
    const size = pet.offsetWidth || 72;
    return { x: r.left + scrollX + r.width * +node.dataset.perch - size / 2, y: r.top + scrollY - size + 6 };
  }

  function walkTo(node) {
    perch = node;
    clearTimeout(actTimer);
    clearTimeout(arriveTimer);
    const to = spot(node);
    const from = at ?? to;
    const dist = Math.hypot(to.x - from.x, to.y - from.y);
    const ms = at ? Math.min(1400, 300 + dist * 1.2) : 0;
    if (to.x !== from.x) face = to.x < from.x ? -1 : 1;
    pet.style.transitionDuration = `${ms}ms`;
    pet.style.transform = `translate(${to.x}px, ${to.y}px) scaleX(${face})`;
    at = to;
    if (ms) play(pet, 'working');
    arriveTimer = setTimeout(() => arrive(node), ms);
  }

  function arrive(node) {
    const clip = ACTS[node.dataset.act] ?? 'delegating';
    play(pet, clip);
    if (node.dataset.act === 'stamp') {
      // The happy hop lands as the stamp does.
      actTimer = setTimeout(() => {
        for (const target of node.querySelectorAll(node.dataset.stamp)) {
          target.classList.remove('stamped'); void target.offsetWidth; target.classList.add('stamped');
        }
      }, index.clips.delivered.durationMs * 0.5);
    }
    if (node.dataset.act === 'patrol') {
      // Pace the card while the agents work, then sit and watch.
      actTimer = setTimeout(() => { if (perch === node) walkTo(node); }, 4200);
    }
  }

  function leave() {
    away = true;
    homeTo(['waking', 'homeless']);
    pet.classList.add('out');
  }
  function comeHome() {
    away = false;
    perch = null;
    at = null;
    pet.classList.remove('out');
    homeTo(['going_back', 'idle']);
  }

  // The perch nearest the upper middle of the screen; the seal near the top.
  function pick() {
    if (scrollY < 40) return null;
    const mid = innerHeight * 0.42;
    let best = null, bd = Infinity;
    for (const node of document.querySelectorAll('[data-perch]')) {
      const r = node.getBoundingClientRect();
      if (r.bottom < 40 || r.top > innerHeight - 40) continue;
      const d = Math.abs(r.top - mid);
      if (d < bd) { bd = d; best = node; }
    }
    return best;
  }

  let queued = false;
  function follow() {
    queued = false;
    const next = pick();
    if (!next) { if (away) comeHome(); return; }
    if (!away) leave();
    if (next !== perch) walkTo(next);
  }
  addEventListener('scroll', () => { if (!queued) { queued = true; setTimeout(follow, 180); } }, { passive: true });
  addEventListener('resize', () => { if (perch) { at = null; walkTo(perch); } });
  follow();
}

// Every other pet, the way it sits when Brain hands work off.
function fillStrip(strip, index, reduce) {
  for (const pet of index.pets) {
    if (pet.id === index.default) continue;
    const item = document.createElement('li');
    const img = document.createElement('img');
    img.src = `${ROOT}${pet.id}/${reduce ? 'portrait' : 'delegating-small'}.webp`;
    img.alt = '';
    img.width = img.height = 64;
    img.loading = 'lazy';
    const name = document.createElement('span');
    name.textContent = pet.name.en;
    item.append(img, name);
    strip.append(item);
  }
}
