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
// Canvas px the pet moves per cycle frame when the drawings don't say: an
// in-place drawing (no paw slides back) spreads this by contact instead.
const STRIDE = { walk: 12, run: 17, stalk: 5 };
// Where the swat row's paw strikes, as a fraction of the drawn size from the feet.
const REACH_PAT = [0.3, -0.31];
// The idle row's frame with its eyes shut: purring. Page px per ms² for a drop.
const PURR = 3, GRAVITY = 0.0026;
// Canvas px a gait lifts at its passing frames, less what the drawings bob.
const BOB = 4;
// ms: the pause on a front-facing frame before a turn, and the settle into and
// out of a walk.
const TURN = 150, SETTLE = 90;

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
    return { ...spec, name, img, ends, total: ends[ends.length - 1], gait: spec.contact && gaitOf(name, spec) };
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
    const v = { canvas, clip: null, frame: 0, flip: false, fade: null, alpha: 1 };
    v.draw = (now) => {
      ctx.clearRect(0, 0, SIDE, SIDE);
      paint(ctx, v.clip, v.frame, v.flip, v.alpha);
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
  // x, y: the feet in page px; z: drawn size; dir: 1 faces right; rot: radians
  // about pivot (canvas px, the feet when null); held: in your hand; placed:
  // where you put it down, kept until it scrolls out of view
  const pet = { x: 0, y: 0, z: 116, dir: 1, lift: 0, bob: 0, rot: 0, pivot: null, away: false, perch: null, frac: null, held: false, placed: null };
  // what it shows: a clip on its own clock, or a frame chosen by the action
  let shown = { name: 'delegating', t0: 0, frame: null };
  function show(name, frame = null, restart = false) {
    if (!ready[name]) return;
    if (name !== gait.name) { gait.name = null; pet.bob = 0; }
    if (frame !== null) frame = Math.min(frame, ready[name].frames - 1); // clips differ in length
    if (restart || shown.name !== name || frame === null && shown.frame !== null) shown = { name, t0: performance.now(), frame };
    else shown.frame = frame;
  }
  // a walk or run whose frame follows the distance covered: each drawing holds
  // for its own measured travel, so the planted paws stay put, and the body
  // bobs with the steps: down on contact, up passing
  let gait = { name: null, frame: 0, into: 0, scale: 1 };
  function stride(name, dist) {
    const c = ready[name];
    if (!c?.gait) return;
    if (gait.name !== name) gait = { name, frame: c.gait.contacts[0], into: 0, scale: 1 };
    gait.into += Math.abs(dist) * SIDE / pet.z * gait.scale;
    const { travel, bob } = c.gait;
    while (gait.into >= travel[gait.frame] - 1e-6) { gait.into -= travel[gait.frame]; gait.frame = (gait.frame + 1) % c.frames; }
    show(name, gait.frame);
    const u = gait.into / travel[gait.frame];
    pet.bob = bob * (1 - lerp(c.contact[gait.frame], c.contact[(gait.frame + 1) % c.frames], u)) * pet.z / SIDE;
  }
  // ms of walking at a mean speed (page px per ms), at most max px: the body
  // goes at the gait's own pace through each drawing (slow while paws are
  // down, quicker in flight), so every drawing stays up for its hold. Returns
  // the px covered.
  function strideFor(name, ms, speed, max = Infinity) {
    const c = ready[name]?.gait;
    if (!c) { const d = Math.min(max, speed * ms); stride(name, d); return d; }
    if (gait.name !== name) stride(name, 0);
    const px = pet.z / SIDE / gait.scale; // page px per gait px
    let moved = 0;
    while (ms > 1e-6 && moved < max) {
      const v = speed * c.pace[gait.frame];
      const d = Math.min((c.travel[gait.frame] - gait.into) * px, max - moved, v * ms);
      ms -= d / v;
      moved += d;
      stride(name, d);
    }
    return moved;
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
  const rest = (name) => Object.assign(act(1e9, () => {
    if (!pet.perch || api.game) return;
    const s = spot(pet.perch);
    pet.x = s.x; pet.y = s.y; pet.z = s.z;
  }, () => show(name)), { rest: true });

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
  // a pat of the paw (the swat row, quicker): raise, reach, the strike held as
  // onHit stamps, back down
  const PAT = [[0.16, 1], [0.34, 2], [0.66, 3], [0.86, 4], [1, 0]];
  function pat(onHit) {
    let hit = false;
    return act(470, (u) => {
      show('swat', PAT.find(([end]) => u < end)?.[1] ?? 0);
      if (!hit && u >= 0.34) {
        hit = true;
        onHit();
        const r = REACH_PAT;
        api.fx?.('ring', pet.x + r[0] * pet.z * pet.dir, pet.y + r[1] * pet.z);
      }
    });
  }
  // walk (or creep, or dash) along the floor to x: a pause facing out before a
  // turn, a settle on the first step, speed easing in over about a step and
  // out over the last, at the gait's own pace, and a stop as a paw comes down
  function walkTo(getX, name = 'walk', speed = 0.09) {
    let x0, d, dir, turn, ramp, vmax, scale, s, t, stopped;
    return act(1e9, function (u, dt) {
      t += dt;
      if (t < turn) { show('delegating'); return; }
      pet.dir = dir;
      if (gait.name !== name) { stride(name, 0); gait.scale = scale; } // after the turn's front frame
      if (t < turn + SETTLE) return stride(name, 0);
      if (s >= d) {
        stride(name, 0);
        stopped ??= t;
        if (t - stopped >= SETTLE) this.ms = 0;
        return;
      }
      // constant acceleration over the first ramp of distance, the same braking
      // over the last
      const e = clamp(Math.min(s, d - s) / ramp, 0, 1);
      s += strideFor(name, dt, vmax * (0.18 + 0.82 * Math.sqrt(e)), d - s);
      pet.x = x0 + dir * s;
    }, function () {
      x0 = pet.x;
      const x1 = getX();
      d = Math.abs(x1 - x0);
      dir = d > 2 ? (x1 > x0 ? 1 : -1) : pet.dir;
      turn = dir !== pet.dir && d > 2 ? TURN : 0;
      vmax = speed * pet.z / 116;
      ramp = Math.max(1, Math.min(d / 2, 0.3 * pet.z));
      s = 0; t = 0; stopped = null;
      // stretch the steps a little so the walk ends as a paw comes down
      if (turn) gait.name = null; // the turn's front frame breaks the step
      stride(name, 0);
      const c = ready[name]?.gait;
      if (c && gait.name === name) {
        const want = d * SIDE / pet.z;
        let at = 0, best = null;
        for (let f = gait.frame; at < want * 1.4 + c.cycle; f = (f + 1) % c.travel.length) {
          at += c.travel[f];
          if (c.contacts.includes((f + 1) % c.travel.length) && (!best || Math.abs(at - want) < Math.abs(best - want))) best = at;
        }
        gait.into = 0;
        gait.scale = best && best / want > 0.75 && best / want < 1.33 ? best / want : 1;
      }
      scale = gait.scale;
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
        const n = document.elementsFromPoint(x - scrollX + dx, vy).find((m) => !m.closest('canvas.pet, .pet-hit, .cat-fx, .toy, .bar'));
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
      // one pat of the paw per mark, in order, each stamping as the paw comes down
      const marks = [...node.querySelectorAll(node.dataset.stamp)];
      const mark = (target) => () => { target.classList.remove('stamped'); void target.offsetWidth; target.classList.add('stamped'); };
      return [call(() => marks.forEach((m) => m.classList.remove('stamped'))), wait(220), ...marks.map((m) => pat(mark(m))), wait(180), rest('delegating')];
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
    if (api.game || pet.held) return true;
    if (busy()) return false;
    // where you put it stays put while you can still see it
    if (pet.placed) {
      const r = pet.placed.getBoundingClientRect();
      if (r.bottom > 60 && r.top < innerHeight) return true;
      pet.placed = null;
    }
    const next = pick();
    if (!next) { if (pet.away) run(comeHome()); }
    else if (next !== pet.perch) run(travel(next));
    return true;
  }
  let scrolled = true;
  addEventListener('scroll', () => { scrolled = true; }, { passive: true });
  addEventListener('resize', () => { scrolled = true; });

  // ---- your hands: pick it up, pet it -------------------------------------------------------
  // Out in the page a small box over its body takes the pointer (the canvas
  // never does, so links around it stay clickable). Press and move to pick it
  // up: it hangs by its raised paws and swings, and when you let go it drops
  // onto whatever is under it (a card, a heading, the hero floor, or back into
  // the seal) with a landing hop. A click is a pat on the head: a hop and a
  // heart. Rubbing it with the mouse makes it purr; the mouse coming over it
  // gets a wave. At home a drag (a long press on touch) lifts it out of the seal.
  const hands = (() => {
    const hit = document.createElement('div');
    hit.className = 'pet-hit';
    hit.setAttribute('aria-hidden', 'true');
    document.body.append(hit);
    let press = null, swing = { a: 0, w: 0, vx: 0 }, grip = [ax, ay], noClick = false;
    let rub = 0, rubT = 0, lastHeart = 0, waveAt = 0, over = false;
    const idle = () => !api.game && !pet.held && current?.rest && !queue.length;
    // the frame it hangs from, and where its paws are: the top of its drawing
    const HANG = ['rear', 3];
    function hang() {
      const c = ready[HANG[0]];
      const frame = Math.min(HANG[1], (c?.frames ?? 1) - 1);
      c.grip ??= [];
      c.grip[frame] ??= topOfFrame(c, frame);
      return [c, frame, c.grip[frame]];
    }
    function pickUp(e) {
      press.drag = true;
      pet.held = true;
      pet.placed = null;
      if (!pet.away) leaveHome();
      run([act(1e9, () => {})]);
      const [c, frame, g] = hang();
      show(HANG[0], frame);
      grip = [ax + (g[0] - ax) * (pet.dir < 0 && !FRONT.has(c.name) ? -1 : 1), g[1]];
      pet.pivot = grip;
      swing = { a: 0, w: 0, vx: 0, x: e.pageX, t: performance.now() };
      document.documentElement.classList.add('pet-carrying');
      move(e);
    }
    // the paws follow the pointer; the body swings below them
    function move(e) {
      const now = performance.now(), dt = Math.max(8, now - swing.t);
      swing.vx = lerp(swing.vx, (e.pageX - swing.x) / dt, 0.35);
      swing.x = e.pageX; swing.t = now;
      const s = pet.z / SIDE;
      pet.x = e.pageX - (grip[0] - ax) * s;
      pet.y = e.pageY - (grip[1] - ay) * s;
    }
    function drop(e) {
      pet.held = false;
      document.documentElement.classList.remove('pet-carrying');
      pet.pivot = null;
      const sr = home.canvas.getBoundingClientRect();
      if (e.clientX > sr.left && e.clientX < sr.right && e.clientY > sr.top && e.clientY < sr.bottom) {
        pet.placed = null;
        run([fall(() => homeSpot()), ...comeHome().slice(1)]);
        return;
      }
      const x = column(pet.x + clamp(swing.vx, -1.5, 1.5) * 140);
      const under = surfaceUnder(x, pet.y - 10);
      let node = under?.node ?? nearest(), to;
      if (under) {
        const r = node.getBoundingClientRect();
        pet.perch = node;
        pet.frac = node === stage ? (x - r.left - scrollX) / r.width : clamp((x - r.left - scrollX) / r.width, 0.06, 0.94);
        to = () => spot(node);
      } else {
        pet.perch = node; pet.frac = null;
        to = () => spot(node);
      }
      pet.placed = node;
      const then = api.game ? [] : node.dataset.act && node.hasAttribute('data-perch') ? arrive(node) : [rest('delegating')];
      run([fall(to), hop(0.16 * pet.z, 380, 'jump'), ...then]);
    }
    // falling under gravity onto to(), leaning back upright on the way
    function fall(getTo) {
      let x0, y0, z0, r0, to;
      return act(400, (u) => {
        to = getTo();
        pet.x = lerp(x0, to.x, u);
        pet.y = lerp(y0, to.y, u * u);
        pet.z = lerp(z0, to.z, u);
        pet.rot = r0 * (1 - ease(Math.min(1, u * 2)));
        show('jump', u < 0.7 ? 2 : 3);
      }, function () {
        x0 = pet.x; y0 = pet.y; z0 = pet.z; r0 = pet.rot; to = getTo();
        this.ms = clamp(Math.sqrt(2 * Math.max(16, to.y - y0) / GRAVITY), 220, 720);
      });
    }
    function poke() {
      const h = headAt(pet, shown.name);
      api.fx?.('heart', h.x, h.y - 0.12 * pet.z);
      if (idle()) run([hop(0.2 * pet.z, 460, 'jump'), rest('delegating')]);
    }
    function purr(now) {
      if (now - lastHeart > 300) {
        lastHeart = now;
        const h = headAt(pet, shown.name);
        api.fx?.('heart', h.x + (Math.random() - 0.5) * 0.2 * pet.z, h.y - 0.1 * pet.z);
      }
      if (idle() || current?.purr) {
        const a = act(900, () => show('delegating', PURR), null);
        a.purr = true;
        run([a, rest('delegating')]);
      }
    }
    function wave(now) {
      if (now - waveAt < 6000 || !idle()) return;
      waveAt = now;
      run([play(ready.wave ? 'wave' : 'attention', 1100), rest('delegating')]);
    }

    function down(e, from) {
      if (e.button > 0) return;
      press = { x: e.clientX, y: e.clientY, t: performance.now(), id: e.pointerId, type: e.pointerType, from, drag: false };
      // at home a finger holds still a moment to lift it, so the hero still scrolls
      if (from === 'home' && e.pointerType !== 'mouse') press.timer = setTimeout(() => { if (press && !press.moved) pickUp(e); }, 380);
    }
    hit.addEventListener('pointerdown', (e) => { e.preventDefault(); down(e, 'hit'); });
    home.canvas.addEventListener('pointerdown', (e) => down(e, 'home'));
    addEventListener('pointermove', (e) => {
      if (!press || e.pointerId !== press.id) return;
      if (press.drag) { move(e); return; }
      if (Math.hypot(e.clientX - press.x, e.clientY - press.y) < 7) return;
      press.moved = true;
      if (press.from === 'hit' || press.type === 'mouse') { clearTimeout(press.timer); pickUp(e); }
      else { clearTimeout(press.timer); press = null; } // a scroll
    }, { passive: true });
    const up = (e) => {
      if (!press || e.pointerId !== press.id) return;
      clearTimeout(press.timer);
      if (press.drag) { noClick = true; setTimeout(() => { noClick = false; }, 0); drop(e); }
      else if (press.from === 'hit' && e.type === 'pointerup') poke();
      press = null;
    };
    addEventListener('pointerup', up);
    addEventListener('pointercancel', up);
    // the click that ends a drag is not a tap on the seal
    home.canvas.addEventListener('click', (e) => { if (noClick) e.stopImmediatePropagation(); }, true);
    // a finger that lifted it doesn't scroll the page while carrying it
    addEventListener('touchmove', (e) => { if (pet.held) e.preventDefault(); }, { passive: false });
    hit.addEventListener('pointermove', (e) => {
      if (e.pointerType !== 'mouse' || press) return;
      const now = performance.now();
      rub = rub * Math.exp(-(now - rubT) / 500) + Math.min(40, Math.abs(e.movementX) + Math.abs(e.movementY));
      rubT = now;
      if (rub > 120) purr(now);
    });
    hit.addEventListener('pointerenter', (e) => { over = true; if (e.pointerType === 'mouse' && !press) wave(performance.now()); });
    hit.addEventListener('pointerleave', () => { over = false; });

    return {
      tick(now, dt) {
        // the swing: a spring towards a lean against the motion, under-damped
        if (pet.held) {
          const k = dt / 1000, target = clamp(swing.vx * 0.9, -0.7, 0.7);
          swing.w += (90 * (target - pet.rot) - 7 * swing.w) * k;
          pet.rot = clamp(pet.rot + swing.w * k, -1, 1);
          swing.vx *= Math.exp(-dt / 120);
          pet.z = lerp(pet.z, awaySize(), Math.min(1, dt / 120));
        }
        const on = pet.away && pet.z > 0;
        hit.hidden = !on;
        if (!on) return;
        const w = 0.44 * pet.z, h = 0.46 * pet.z;
        hit.style.transform = `translate3d(${(pet.x - w / 2).toFixed(1)}px, ${(pet.y - pet.lift - pet.bob - h).toFixed(1)}px, 0)`;
        hit.style.width = `${w.toFixed(1)}px`;
        hit.style.height = `${h.toFixed(1)}px`;
      },
    };
  })();

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
      const s = pet.z / SIDE, [cx, cy] = pet.pivot ?? [ax, ay];
      const px = pet.x + (cx - ax) * s, py = pet.y - pet.lift - pet.bob + (cy - ay) * s;
      out.canvas.style.transform = `translate3d(${px.toFixed(1)}px, ${py.toFixed(1)}px, 0) rotate(${pet.rot.toFixed(3)}rad) scale(${s.toFixed(4)}) translate(${-cx}px, ${-cy}px)`;
    }
    hands.tick(now, dt);
    api.after?.(now, dt);
    requestAnimationFrame(tick);
  }
  const api = {
    pet, ready, need, home: home.canvas, stage, game: null, onHomeTap: null, after: null, fx: null,
    show, stride, strideFor, run, queue: () => queue, busy: () => !!current || queue.length > 0,
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

// The top of a frame's drawing in canvas px (x at the middle of its top rows):
// where a pet hangs from when you pick it up by its raised paws.
function topOfFrame(clip, frame) {
  const c = document.createElement('canvas');
  c.width = c.height = SIDE;
  const ctx = c.getContext('2d', { willReadFrequently: true });
  ctx.drawImage(clip.img, frame * SIDE, 0, SIDE, SIDE, 0, 0, SIDE, SIDE);
  const a = ctx.getImageData(0, 0, SIDE, SIDE).data;
  for (let y = 0; y < SIDE; y++) {
    let sx = 0, n = 0;
    for (let yy = y; yy < Math.min(SIDE, y + 10); yy++) for (let x = 0; x < SIDE; x++) if (a[(yy * SIDE + x) * 4 + 3] > 60) { sx += x; n++; }
    if (n > 12) return [sx / n, y + 6];
  }
  return [SIDE / 2, SIDE / 2];
}

// Where the head is in each clip, as a fraction of the drawn size from the
// feet, facing right: what the toys aim at and the scores pop over.
const HEAD = {
  working: [0.22, -0.45], run: [0.24, -0.42], stalk: [0.3, -0.22], wiggle: [0.3, -0.25], pounce: [0.2, -0.2],
  swat: [-0.08, -0.45], rear: [0.03, -0.69], jump: [-0.12, -0.62], eat: [-0.06, -0.2], groom: [-0.1, -0.45],
};
function headAt(pet, name) {
  const [hx, hy] = HEAD[name] ?? [0, -0.6];
  return { x: pet.x + hx * pet.z * pet.dir, y: pet.y - pet.lift + hy * pet.z };
}

// A moving clip's travel per frame (canvas px), measured from its drawings by
// scripts/import-pets.py; an in-place drawing gets its cycle's STRIDE spread by
// contact, little while paws are down and most in flight. Its contact frames,
// its pace (speed per frame that keeps each drawing up for its hold), and the
// bob the page adds.
function gaitOf(name, spec) {
  const n = spec.frames;
  let travel = spec.travel;
  if (!travel) {
    const w = spec.contact.map((c) => 0.25 + (1 - c));
    const sum = w.reduce((a, b) => a + b, 0);
    travel = w.map((x) => x * (STRIDE[name] ?? 12) * n / sum);
  }
  // each drawing's hold, the loop's long last hold capped
  const mid = [...spec.durations].sort((a, b) => a - b)[n >> 1];
  const holds = spec.durations.map((d) => Math.min(d, mid * 1.5));
  const cycle = travel.reduce((a, b) => a + b, 0), time = holds.reduce((a, b) => a + b, 0);
  const most = Math.max(...spec.contact);
  const contacts = spec.contact.map((c, i) => (c >= 0.6 * most ? i : -1)).filter((i) => i >= 0);
  return {
    travel, cycle, contacts: contacts.length ? contacts : [0],
    pace: travel.map((t, i) => (t / holds[i]) / (cycle / time)),
    bob: Math.max(0, BOB - Math.max(...spec.rise)),
  };
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
