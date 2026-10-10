// The landing's pet: the default pet drawn frame by frame from sprite strips
// in pets/ (written by scripts/import-pets.py from the same registered frames
// the app plays). Every strip is decoded before it is first shown, so a clip
// change never shows a blank frame. One rAF loop moves and draws it:
// - home: asleep in the hero seal, wakes to watch its team orbit, hops when
//   tapped;
// - story: when you scroll it leaves the seal empty and leaps from card to
//   card, acting out each one (data-act);
// - play: play.js drives it through the same actions (see game()).
const ROOT = 'pets/';
const SIDE = 256;
const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
const lerp = (a, b, t) => a + (b - a) * t;
const ease = (t) => t * t * (3 - 2 * t);

// What each story card asks of it.
const ACTS = { wake: 'delivered', stamp: 'delivered', patrol: 'working', perk: 'rear', sleep: 'idle' };
// Clips drawn facing the viewer, or with the seal: never mirrored.
const FRONT = new Set(['homeless', 'offline', 'waking', 'idle', 'working', 'delegating', 'attention', 'delivered', 'going_back']);
// Canvas px the pet moves per frame of a cycle, so its paws stay planted.
const STRIDE = { walk: 12, run: 17, stalk: 5 };

export async function startPet(homeImg, strip) {
  const index = await (await fetch(`${ROOT}index.json`)).json();
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
  fillStrip(strip, index, reduce);
  if (reduce) return null;

  const [ax, ay] = index.anchor;
  const clips = {};
  const load = (name) => clips[name] ??= (async () => {
    const spec = index.clips[name];
    const img = new Image();
    img.src = `${ROOT}${index.default}/sprites/${name}.webp`;
    await img.decode();
    const ends = [];
    spec.durations.reduce((t, d) => (ends.push(t + d), t + d), 0);
    return { ...spec, name, img, ends, total: ends[ends.length - 1] };
  })();
  const ready = {};
  const need = async (name) => (ready[name] = await load(name));
  await Promise.all(['idle', 'waking', 'delegating', 'delivered', 'going_back', 'homeless'].map(need));
  // the rest decode in the background; until then the current clip stays up
  for (const name of Object.keys(index.clips)) need(name).catch(() => {});

  // ---- drawing: a sprite view is a 256 px canvas scaled into place --------------
  function view(canvas) {
    canvas.width = canvas.height = SIDE;
    const ctx = canvas.getContext('2d');
    const v = { canvas, clip: null, frame: 0, flip: false, fade: null };
    v.draw = (now) => {
      ctx.clearRect(0, 0, SIDE, SIDE);
      paint(ctx, v.clip, v.frame, v.flip, 1);
      // the clip it left fades out over the new one (the seal behind a hop)
      if (v.fade) {
        const u = (now - v.fade.t0) / 260;
        if (u >= 1) v.fade = null;
        else paint(ctx, v.fade.clip, v.fade.frame, v.fade.flip, 1 - u);
      }
    };
    return v;
  }
  function paint(ctx, clip, frame, flip, alpha) {
    if (!clip) return;
    ctx.save();
    ctx.globalAlpha = alpha;
    if (flip) { ctx.translate(SIDE, 0); ctx.scale(-1, 1); }
    ctx.drawImage(clip.img, frame * SIDE, 0, SIDE, SIDE, 0, 0, SIDE, SIDE);
    ctx.restore();
  }
  // the frame a clip shows t ms in, honouring each frame's own hold
  function frameAt(clip, t) {
    if (clip.loop) t %= clip.total;
    else if (t >= clip.total) return clip.frames - 1;
    let i = 0;
    while (clip.ends[i] <= t) i++;
    return i;
  }

  // ---- home: the hero seal ------------------------------------------------------
  const home = view(document.createElement('canvas'));
  home.canvas.className = homeImg.className;
  home.canvas.setAttribute('role', 'img');
  home.canvas.setAttribute('aria-label', homeImg.alt);
  homeImg.replaceWith(home.canvas);
  let homeSteps = [], homeT0 = 0, homeTimer = 0;
  function homeTo(steps, fade = false) {
    const next = ready[steps[0]];
    if (!next) return;
    if (fade && home.clip) home.fade = { clip: home.clip, frame: home.frame, flip: false, t0: performance.now() };
    home.clip = next;
    homeSteps = steps.slice(1);
    homeT0 = performance.now();
  }
  function homeTick(now) {
    const t = now - homeT0;
    home.frame = frameAt(home.clip, t);
    if (homeSteps.length && t >= home.clip.total) homeTo(homeSteps, true);
    home.draw(now);
  }
  homeTo(['idle']);
  homeTimer = setTimeout(() => homeTo(['waking', 'delegating']), 2200);
  home.canvas.addEventListener('click', () => {
    clearTimeout(homeTimer);
    if (pet.away) return;
    if (home.clip.name === 'idle') homeTo(['waking', 'delegating']);
    else homeTo(['delivered', 'delegating']);
    api.onHomeTap?.();
  });
  // where a cat at home stands: the seal's ground line, at the seal's size
  function homeSpot() {
    const r = home.canvas.getBoundingClientRect();
    return { x: r.left + scrollX + r.width * ax / SIDE, y: r.top + scrollY + r.height * ay / SIDE, z: r.width };
  }

  // ---- the pet out in the page --------------------------------------------------
  const out = view(document.createElement('canvas'));
  out.canvas.className = 'pet';
  out.canvas.setAttribute('aria-hidden', 'true');
  document.body.append(out.canvas);
  const awaySize = () => (innerWidth < 600 ? 92 : 116);
  // x, y: the feet in page px; z: drawn size; dir: 1 faces right
  const pet = { x: 0, y: 0, z: 116, dir: 1, lift: 0, away: false, perch: null, frac: null };
  // what it shows: a clip on its own clock, or a frame chosen by the action
  let shown = { name: 'delegating', t0: 0, frame: null };
  function show(name, frame = null, restart = false) {
    if (!ready[name]) return;
    if (frame !== null) frame = Math.min(frame, ready[name].frames - 1); // clips differ in length
    if (restart || shown.name !== name || frame === null && shown.frame !== null) shown = { name, t0: performance.now(), frame };
    else shown.frame = frame;
  }
  // a walk or run whose frame follows the distance covered
  let walked = 0;
  function stride(name, dist) {
    walked += Math.abs(dist) * SIDE / pet.z;
    show(name, Math.floor(walked / STRIDE[name]) % (ready[name]?.frames ?? 8));
  }

  // ---- actions: a queue of timed steps (u from 0 to 1) ----------------------------
  let queue = [], current = null;
  const act = (ms, step, init) => ({ ms, step, init });
  const run = (list) => { queue = list; current = null; };
  const call = (fn) => act(1, () => {}, fn);
  const wait = (ms) => act(ms, () => {});
  // a clip from its first frame, for ms or once through; step(u) runs alongside
  const play = (name, ms, step) => act(ms ?? 1, step ?? (() => {}), function () {
    show(name, null, true);
    if (ms == null) this.ms = ready[name]?.total ?? 400;
  });
  // hold a clip until something else comes up, keeping to the perch
  const rest = (name) => act(1e9, () => {
    if (!pet.perch || api.game) return;
    const s = spot(pet.perch);
    pet.x = s.x; pet.y = s.y; pet.z = s.z;
  }, () => show(name));

  // A leap to wherever getTo() says (x, y, z), along an arc. The pounce clip
  // for a leap across, the jump clip for one mostly up or down; its frames
  // follow the flight: crouch, take off, fly, land.
  function leap(getTo, { h, clip } = {}) {
    let x0, y0, z0, to, height, name;
    return Object.assign(act(600, (u) => {
      const c = ready[name];
      const fly = clamp((u - 0.16) / 0.68, 0, 1);
      to = getTo(); // a card still sliding into place moves its landing spot
      pet.x = lerp(x0, to.x, ease(fly));
      pet.y = lerp(y0, to.y, fly) - height * 4 * fly * (1 - fly);
      pet.z = lerp(z0, to.z, ease(fly));
      // crouch 0-1, launch 2, air 3-4, land 5-7
      const f = u < 0.16 ? (u < 0.08 ? 0 : 1) : u < 0.84 ? 2 + Math.min(2, Math.floor(fly * 3)) : 5 + Math.min(2, Math.floor((u - 0.84) / 0.16 * 3));
      if (c) show(name, Math.min(c.frames - 1, f));
    }, function () {
      x0 = pet.x; y0 = pet.y; z0 = pet.z; to = getTo();
      const dx = to.x - x0, dy = to.y - y0, d = Math.hypot(dx, dy);
      if (Math.abs(dx) > 4) pet.dir = dx > 0 ? 1 : -1;
      name = clip ?? (Math.abs(dy) > Math.abs(dx) * 1.2 ? 'jump' : 'pounce');
      if (!ready[name]) name = 'pounce';
      height = h ?? clamp(40 + d * 0.22, 40, 220) * (dy < 0 ? 1.25 : 1);
      this.ms = h != null ? clamp(380 + d * 0.5, 380, 640) : clamp(560 + d * 0.32, 560, 1150);
    }), { leap: true });
  }
  // a straight-up hop that lands where it took off; onStep(u) for hit tests
  function hop(h, ms, clip, onStep) {
    let y0;
    return act(ms, (u) => {
      const fly = clamp((u - 0.2) / 0.6, 0, 1);
      pet.y = y0 - h * 4 * fly * (1 - fly);
      const c = ready[clip];
      const n = c?.frames ?? 8;
      // anticipation, the reach at the top, then the landing frames
      const f = u < 0.2 ? Math.floor(u / 0.2 * 2) : u < 0.8 ? 2 + Math.floor(fly * (n - 5)) : n - 3 + Math.floor((u - 0.8) / 0.2 * 3);
      show(clip, clamp(f, 0, n - 1));
      onStep?.(u, fly);
    }, () => { y0 = pet.y; });
  }
  // walk (or creep, or dash) along the floor to x
  function walkTo(getX, name = 'walk', speed = 0.09) {
    let x1;
    return act(400, (u, dt) => {
      const dx = x1 - pet.x;
      const step = Math.sign(dx) * Math.min(Math.abs(dx), speed * dt * pet.z / 116);
      pet.x += step;
      stride(name, step);
    }, function () {
      x1 = getX();
      if (Math.abs(x1 - pet.x) > 2) pet.dir = x1 > pet.x ? 1 : -1;
      this.ms = Math.max(120, Math.abs(x1 - pet.x) / (speed * pet.z / 116));
    });
  }

  // ---- where things are -----------------------------------------------------------
  const stage = home.canvas.closest('.stage') ?? home.canvas.parentElement;
  // the surface a node offers to stand on: the hero's is the seal's ground line
  function topOf(node) {
    if (node === stage) return homeSpot().y;
    return node.getBoundingClientRect().top + scrollY + 1;
  }
  function spot(node) {
    if (node === stage) {
      const s = homeSpot(), r = stage.getBoundingClientRect();
      const f = node === pet.perch && pet.frac != null ? pet.frac : null;
      return { x: f == null ? s.x : r.left + scrollX + r.width * f, y: s.y, z: awaySize() };
    }
    const r = node.getBoundingClientRect();
    const f = node === pet.perch && pet.frac != null ? pet.frac : +(node.dataset.perch || 0.5);
    return { x: r.left + scrollX + r.width * f, y: r.top + scrollY + 1, z: awaySize() };
  }
  // the strip of floor it may walk on
  function floor() {
    const node = pet.perch;
    if (!node) return { a: scrollX + 30, b: scrollX + innerWidth - 30 };
    const r = node.getBoundingClientRect(), pad = pet.z * 0.32;
    return { a: r.left + scrollX + pad, b: r.right + scrollX - pad };
  }
  // look straight down from a page point for the first thing to stand on
  const SOLID = 'a.btn, button, h1, h2, h3, p, li, pre, figure, aside, [data-perch], .vis, .ctas, .runs, .works, .steps, .trust, .end-card, .stage';
  function surfaceUnder(x, y) {
    for (let vy = Math.max(70, y - scrollY); vy < innerHeight - 4; vy += 6) {
      let solid = null;
      for (const dx of [0, -22, 22]) {
        const n = document.elementsFromPoint(x - scrollX + dx, vy).find((m) => !m.closest('canvas.pet, .cat-fx, .toy, .bar'));
        if ((solid = n?.closest(SOLID))) break;
      }
      if (!solid) continue;
      const top = topOf(solid);
      if (top - scrollY > 70 && top >= y - 30) return { node: solid, y: top };
    }
    return null;
  }
  // the x a leaping pet may come down at: inside the content column
  function column(x) {
    const col = document.querySelector('main .wrap')?.getBoundingClientRect() ?? { left: 0, right: innerWidth };
    return clamp(x - scrollX, Math.max(30, col.left + 56), Math.min(innerWidth - 30, col.right - 56)) + scrollX;
  }

  // ---- the story --------------------------------------------------------------------
  // asleep at home: wake up first
  const wakeHome = () => act(1, () => {}, function () {
    clearTimeout(homeTimer);
    if (home.clip.name !== 'idle' && home.clip.name !== 'going_back') return;
    homeTo(['waking', 'delegating']);
    this.ms = ready.waking.total;
  });
  function leaveHome() {
    pet.away = true;
    clearTimeout(homeTimer);
    const s = homeSpot();
    Object.assign(pet, { x: s.x, y: s.y, z: s.z, dir: 1 });
    // the cat steps out of the drawing; the seal stays, empty
    show(home.clip.name === 'idle' || home.clip.name === 'waking' ? 'delegating' : home.clip.name);
    homeTo(['homeless'], true);
    out.canvas.classList.add('out');
  }
  function comeHome() {
    return [leap(() => homeSpot(), { clip: 'jump' }), call(() => {
      pet.away = false;
      pet.perch = null;
      out.canvas.classList.remove('out');
      homeTo(['going_back', 'idle'], true);
    })];
  }
  function arrive(node) {
    const job = node.dataset.act;
    if (job === 'stamp') {
      // the happy hop lands as the stamp does
      const marks = [...node.querySelectorAll(node.dataset.stamp)];
      let stamped = false;
      return [play('delivered', null, (u) => {
        if (stamped || u < 0.5) return;
        stamped = true;
        for (const target of marks) { target.classList.remove('stamped'); void target.offsetWidth; target.classList.add('stamped'); }
      }), rest('delegating')];
    }
    if (job === 'patrol') {
      const edge = (f) => () => { const r = node.getBoundingClientRect(); return r.left + scrollX + r.width * f; };
      return [walkTo(edge(0.14)), walkTo(edge(0.86)), walkTo(edge(0.5)), rest('delegating')];
    }
    if (job === 'sleep') return [rest('idle')];
    return [play(ACTS[job] ?? 'delegating'), rest('delegating')];
  }
  function travel(node, quiet = false) {
    pet.perch = node;
    pet.frac = null;
    const out = [];
    if (!pet.away) out.push(wakeHome(), call(leaveHome));
    out.push(leap(() => spot(node)), call(() => { queue.unshift(...(quiet ? [] : arrive(node))); }));
    return out;
  }
  // the perch nearest the upper middle of the screen; home near the top
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
  function nearest() {
    const mid = innerHeight * 0.42;
    let best = stage, bd = Math.abs(stage.getBoundingClientRect().top - mid);
    for (const node of document.querySelectorAll('[data-perch]')) {
      const d = Math.abs(node.getBoundingClientRect().top - mid);
      if (d < bd) { bd = d; best = node; }
    }
    return best;
  }
  // a leap in flight finishes before the next one starts
  const busy = () => current?.leap || queue.some((a) => a.leap);
  // false while a leap is under way: ask again once it lands
  function follow() {
    if (api.game) return true;
    if (busy()) return false;
    const next = pick();
    if (!next) { if (pet.away) run(comeHome()); }
    else if (next !== pet.perch) run(travel(next));
    return true;
  }
  let scrolled = true;
  addEventListener('scroll', () => { scrolled = true; }, { passive: true });
  addEventListener('resize', () => { scrolled = true; });

  // ---- the loop ------------------------------------------------------------------------
  let last = performance.now();
  function tick(now) {
    const dt = Math.min(50, now - last);
    last = now;
    homeTick(now);
    if (!current && queue.length) { current = queue.shift(); current.init?.(); current.t0 = now; }
    if (current) {
      const u = clamp((now - current.t0) / current.ms, 0, 1);
      current.step(u, dt);
      if (u >= 1) current = null;
    }
    if (api.game) api.game(now, dt);
    if (scrolled && follow()) scrolled = false;
    if (pet.away) {
      const clip = ready[shown.name];
      out.clip = clip;
      out.frame = shown.frame ?? frameAt(clip, now - shown.t0);
      out.flip = pet.dir < 0 && !FRONT.has(clip.name);
      out.draw(now);
      const s = pet.z / SIDE;
      out.canvas.style.transform = `translate3d(${(pet.x - ax * s).toFixed(1)}px, ${(pet.y - pet.lift - ay * s).toFixed(1)}px, 0) scale(${s.toFixed(4)})`;
    }
    api.after?.(now, dt);
    requestAnimationFrame(tick);
  }
  const api = {
    pet, ready, need, home: home.canvas, stage, game: null, onHomeTap: null, after: null,
    show, stride, run, queue: () => queue, busy: () => !!current || queue.length > 0,
    push: (...steps) => queue.push(...steps), unshift: (...steps) => queue.unshift(...steps),
    act, call, wait, play, rest, leap, hop, walkTo, leaveHome, comeHome, travel,
    spot, floor, topOf, surfaceUnder, column, pick, nearest, awaySize,
    head(name = shown.name) { return headAt(pet, name); },
    refollow() { scrolled = true; },
    clip: () => shown.name,
  };
  requestAnimationFrame(tick);
  return api;
}

// Where the head is in each clip, as a fraction of the drawn size from the
// feet, facing right: what the toys aim at and the scores pop over.
const HEAD = {
  working: [0.22, -0.45], run: [0.24, -0.42], stalk: [0.3, -0.22], wiggle: [0.3, -0.25], pounce: [0.26, -0.3],
  swat: [0.15, -0.48], rear: [0.1, -0.62], jump: [0.1, -0.62], eat: [0.3, -0.22], groom: [0.15, -0.48],
};
function headAt(pet, name) {
  const [hx, hy] = HEAD[name] ?? [0, -0.6];
  return { x: pet.x + hx * pet.z * pet.dir, y: pet.y - pet.lift + hy * pet.z };
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
