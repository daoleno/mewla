// Playtime: four toys, each a small game played with the pet from pet.js.
// You tease, it plays: yarn and feather to bat at, a laser dot to stalk and
// pounce on, treats to toss and catch. Hits, catches and snacks add up; hits
// in a row make a streak, and a miss or a dodge ends it. The toys and the
// little marks over the pet are SVG in page coordinates.
const NS = 'http://www.w3.org/2000/svg';
const RED = '#c8372b', RED2 = '#a92b21', PAPER = '#fbf6ec', TREAT = '#d9902f';
const TAU = Math.PI * 2;
const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
const lerp = (a, b, t) => a + (b - a) * t;
const ease = (t) => t * t * (3 - 2 * t);
const back = (t) => 1 + 2.4 * (t - 1) ** 3 + 1.4 * (t - 1) ** 2;
const near = (p, x, y, r) => Math.hypot(p.x - x, p.y - y) < r;

function el(tag, attrs = {}, parent = null) {
  const node = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  parent?.append(node);
  return node;
}

// onEvent hears every playtime event: start, hit, snack, miss, sfx and stop.
export function startPlay(P, onEvent) {
  const pet = P.pet;
  const fx = el('svg', { class: 'cat-fx', width: 1, height: 1, 'aria-hidden': 'true' }, document.body);
  const mouse = { x: -1e4, y: -1e4, cx: -1e4, cy: -1e4, t: 0 };
  const game = {
    mode: null, auto: true, t: 0, ax: 0, ay: 0, x: 0, y: 0, vx: 0, vy: 0, spin: 0,
    speed: 0, stillAt: 0, phase: 'watch', wig: null, pin: null, hideUntil: 0, held: 0,
    hits: 0, misses: 0, streak: 0, best: 0, flops: 0, run: 0, busyUntil: 0, away: 0, outAt: 0,
    treats: [], belly: 0, fullUntil: 0, demoAt: 0,
  };
  // the pet's size relative to the old drawn cat, for distances
  const K = () => pet.z / 116;

  // ---- effects ---------------------------------------------------------------
  const sparks = [];
  const LIFE = { ring: 500, burst: 360, whiff: 380, say: 950, big: 1400, crumb: 650, fluff: 1700 };
  function spark(kind, x, y, text) {
    const g = el('g', {}, fx);
    if (kind === 'heart') el('path', { d: 'M 0 4 C -6 -1 -5 -7 0 -3.6 C 5 -7 6 -1 0 4 Z', fill: RED }, g);
    else if (kind === 'say' || kind === 'big') {
      // a paper outline keeps the word legible on light and dark sections
      el('text', { 'text-anchor': 'middle', fill: RED, stroke: PAPER, 'stroke-width': 4, 'stroke-linejoin': 'round', 'paint-order': 'stroke', 'font-weight': 800, 'font-size': kind === 'big' ? 22 : 15 }, g).textContent = text;
    } else if (kind === 'whiff') el('path', { d: 'M -12 6 Q -2 -12 14 -2 M -8 10 Q 2 -4 14 4', fill: 'none', stroke: RED, 'stroke-width': 1.6, 'stroke-linecap': 'round' }, g);
    else if (kind === 'crumb') el('circle', { r: 1.2 + Math.random(), fill: TREAT }, g);
    else if (kind === 'fluff') el('path', { d: 'M 0 -5 C 2.6 -2 2.4 2 0 5 C -2.4 2 -2.6 -2 0 -5 Z', fill: RED, opacity: 0.85 }, g);
    else if (kind === 'burst') {
      const a0 = Math.random() * TAU, d = Array.from({ length: 6 }, (_, i) => {
        const a = a0 + (i * TAU) / 6, c = Math.cos(a), s = Math.sin(a);
        return `M ${(c * 6).toFixed(1)} ${(s * 6).toFixed(1)} L ${(c * 11).toFixed(1)} ${(s * 11).toFixed(1)}`;
      }).join(' ');
      el('path', { d, fill: 'none', stroke: RED, 'stroke-width': 2, 'stroke-linecap': 'round' }, g);
    } else el('rect', { x: -7, y: -7, width: 14, height: 14, rx: 2, fill: 'none', stroke: RED, 'stroke-width': 2 }, g);
    sparks.push({ g, kind, x, y, t0: performance.now(), dx: (Math.random() - 0.5) * 18, dir: pet.dir, vx: (Math.random() - 0.5) * 0.12, vy: -0.08 - Math.random() * 0.1 });
  }
  function drawSparks(now) {
    for (let i = sparks.length - 1; i >= 0; i--) {
      const s = sparks[i], ms = now - s.t0, u = ms / (LIFE[s.kind] ?? 1100);
      if (u >= 1) { s.g.remove(); sparks.splice(i, 1); continue; }
      let tr, op = 1 - u * u;
      if (s.kind === 'burst') tr = `translate(${s.x} ${s.y}) scale(${(0.8 + u * 1.2).toFixed(2)})`;
      else if (s.kind === 'ring') tr = `translate(${s.x} ${s.y}) scale(${1 + u * 2.4})`;
      else if (s.kind === 'whiff') tr = `translate(${s.x} ${s.y}) scale(${(s.dir * (1 + u * 0.5)).toFixed(2)} ${(1 + u * 0.5).toFixed(2)})`;
      else if (s.kind === 'say' || s.kind === 'big') {
        const popIn = u < 0.18 ? back(u / 0.18) : 1;
        tr = `translate(${s.x.toFixed(1)} ${(s.y - 26 * ease(u)).toFixed(1)}) scale(${popIn.toFixed(3)})`;
        op = u < 0.7 ? 1 : 1 - (u - 0.7) / 0.3;
      } else if (s.kind === 'crumb') tr = `translate(${(s.x + s.vx * ms).toFixed(1)} ${(s.y + s.vy * ms + 0.0009 * ms * ms).toFixed(1)})`;
      else if (s.kind === 'fluff') tr = `translate(${(s.x + s.dx * u + Math.sin(u * 7) * 9).toFixed(1)} ${(s.y + 46 * u).toFixed(1)}) rotate(${(Math.sin(u * 5) * 50).toFixed(1)})`;
      else tr = `translate(${(s.x + s.dx * u).toFixed(1)} ${(s.y - 34 * u).toFixed(1)}) scale(${(0.7 + 0.5 * Math.sin(u * Math.PI)).toFixed(2)})`;
      s.g.setAttribute('transform', tr);
      s.g.setAttribute('opacity', op.toFixed(2));
    }
  }

  // ---- the toys, drawn in page coordinates -------------------------------------
  const toyG = {};
  for (const m of ['yarn', 'feather', 'laser', 'treats']) toyG[m] = el('g', { display: 'none' }, fx);
  const string = el('path', { fill: 'none', stroke: RED2, 'stroke-width': 1.2, 'stroke-linecap': 'round' }, toyG.yarn);
  const ball = el('g', {}, toyG.yarn);
  el('circle', { r: 8, fill: RED }, ball);
  el('path', { d: 'M -6 -3 Q 0 -8 6 -3 M -7 2 Q 0 -3 7 2 M -5 6 Q 0 2 5 6', fill: 'none', stroke: PAPER, 'stroke-width': 1, opacity: 0.8 }, ball);
  const wand = el('path', { fill: 'none', stroke: RED2, 'stroke-width': 1, 'stroke-linecap': 'round' }, toyG.feather);
  const plume = el('g', {}, toyG.feather);
  el('path', { d: 'M 0 0 C 7 5 8 18 1 32 C -7 21 -7 8 0 0 Z', fill: RED }, plume);
  el('path', { d: 'M 0 -2 L 1 33', fill: 'none', stroke: RED2, 'stroke-width': 1.1, 'stroke-linecap': 'round' }, plume);
  el('path', { d: 'M 0.6 9 L 5 6 M 0.8 15 L 5.8 12 M 0.9 21 L 4.6 18.6 M 0.4 12 L -4.2 9.4 M 0.7 18 L -4.6 15.6 M 0.9 24 L -3 22.6', fill: 'none', stroke: PAPER, 'stroke-width': 0.9, 'stroke-linecap': 'round', opacity: 0.85 }, plume);
  const dot = el('g', {}, toyG.laser);
  el('circle', { r: 11, fill: '#ff3b2a', opacity: 0.14 }, dot);
  el('circle', { r: 6, fill: '#ff3b2a', opacity: 0.3 }, dot);
  el('circle', { r: 3.4, fill: '#ff2a1a' }, dot);
  el('circle', { r: 1.3, fill: '#fff', opacity: 0.9 }, dot);
  function treatShape() {
    const g = el('g', {}, toyG.treats);
    el('path', { d: 'M -7 0 C -4 -4.6 3 -5 6 0 C 3 5 -4 4.6 -7 0 Z', fill: TREAT }, g);
    el('path', { d: 'M 5 0 L 9.5 -3.6 L 9.5 3.6 Z', fill: TREAT }, g);
    el('circle', { cx: -4, cy: -0.8, r: 0.9, fill: '#5a3410' }, g);
    el('path', { d: 'M -1 -2.6 Q 0 0 -1 2.6 M 1.6 -2.4 Q 2.5 0 1.6 2.4', fill: 'none', stroke: '#f6d8a8', 'stroke-width': 0.9, 'stroke-linecap': 'round' }, g);
    return g;
  }

  // ---- input -------------------------------------------------------------------
  const lift = () => (game.mode === 'yarn' || game.mode === 'feather' ? 60 : 0);
  addEventListener('pointermove', (e) => {
    mouse.cx = e.clientX; mouse.cy = e.clientY;
    mouse.x = e.pageX; mouse.y = e.pageY; mouse.t = performance.now();
    if (!game.mode) return;
    if (e.pointerType === 'mouse') aim(e.pageX, e.pageY);
    // a finger steers too, until the page takes the gesture as a scroll
    else if (e.buttons && !e.target.closest('a, button, input, .toy')) aim(e.pageX, e.pageY - lift());
  }, { passive: true });
  addEventListener('scroll', () => { mouse.x = mouse.cx + scrollX; mouse.y = mouse.cy + scrollY; }, { passive: true });
  // a click (or a tap) on the page aims the toy there, or tosses a treat
  addEventListener('pointerdown', (e) => {
    if (!game.mode || e.button > 0 || e.target.closest('a, button, input, .toy')) return;
    if (e.pointerType !== 'mouse') aim(e.pageX, e.pageY - lift());
    if (game.mode === 'treats') toss(e.pageX, e.pageY);
  }, { passive: true });

  function aim(x, y) { game.ax = x; game.ay = y; game.auto = false; game.t = performance.now(); }
  const steered = (now) => !game.auto && now - game.t < 2600;
  const emit = (type, extra) => onEvent?.({ type, mode: game.mode, hits: game.hits, misses: game.misses, streak: game.streak, best: game.best, ...extra });
  const missText = () => (steered(performance.now()) ? 'Dodged!' : 'Miss');

  // where a strike lands, in page coordinates: the raised front paw at the top
  // of each clip's swing, measured from the frames as a fraction of the drawn
  // size from the feet; or the mouth
  const REACH = { swat: [0, -0.32], rear: [-0.1, -0.66], jump: [0, -0.62], pounce: [0.31, -0.28] };
  const at = ([rx, ry]) => ({ x: pet.x + rx * pet.z * pet.dir, y: pet.y - pet.lift + ry * pet.z });
  const pawTip = () => at(REACH[P.clip()] ?? [0.3, -0.3]);
  const mouthAt = () => at(P.clip() === 'jump' ? [-0.04, -0.5] : P.clip() === 'eat' ? [0.05, -0.05] : [0.05, -0.42]);

  function score(kind, text, extra) {
    // kind: hit builds the streak, snack counts without it, miss ends it
    if (kind !== 'miss') game.hits++;
    if (kind === 'hit') { game.streak++; game.best = Math.max(game.best, game.streak); game.flops = 0; } else game.streak = 0;
    if (kind === 'miss') { game.misses++; game.flops++; game.run = 0; }
    const s = game.streak, big = kind === 'hit' && (s === 3 || s === 5 || (s >= 10 && s % 5 === 0));
    const h = P.head();
    spark(big ? 'big' : 'say', h.x, h.y - 0.3 * pet.z, kind === 'hit' && s > 1 ? `${text} ×${s}` : text);
    if (big) for (let i = 0; i < 3; i++) spark('heart', h.x + (i - 1) * 16, h.y - 0.14 * pet.z);
    emit(kind, { big, ...extra });
  }

  // walk, creep or dash along the floor toward x; true once there
  function stepTo(x, speed, dt, clip = 'walk') {
    const f = P.floor(), want = clamp(x, f.a, f.b), dx = want - pet.x;
    if (Math.abs(dx) < 3) return true;
    pet.dir = dx > 0 ? 1 : -1;
    const step = Math.sign(dx) * Math.min(Math.abs(dx), speed * dt * K());
    pet.x += step;
    P.stride(clip, step);
    return false;
  }
  const sit = () => P.show('swat', 0);
  // a strike is tested on every frame of the swing; the first contact counts
  function strike(test, onHit) {
    const s = { done: false };
    s.check = () => { if (!s.done && test()) { s.done = true; onHit?.(); } };
    s.miss = (fn) => P.call(() => { if (!s.done) fn(); });
    return s;
  }
  const groom = () => [P.play('groom'), P.call(sit)];

  // jump to whatever is under a page point, if it isn't this floor
  function leapTo(x0, y, node = null) {
    const x = P.column(x0);
    if (!node) node = P.surfaceUnder(x, y)?.node;
    if (!node || node === pet.perch) return false;
    const r = node.getBoundingClientRect();
    if (r.top > innerHeight || r.bottom < 60) return false;
    P.run([P.call(() => {
      pet.perch = node;
      const b = node.getBoundingClientRect();
      pet.frac = clamp((x - b.left - scrollX) / b.width, 0, 1);
    }), P.leap(() => P.spot(node)), P.call(sit)]);
    return true;
  }
  // the toy stays out of reach for a moment: walk to the edge, then leap toward it
  function leapToward(now, x, y) {
    const f = P.floor(), want = clamp(x, f.a, f.b), up = pet.y - y;
    const away = steered(now) && (want !== x || up < -40 * K() || up > 250 * K());
    if (!away) { game.away = 0; return false; }
    if (!game.away) { game.away = now; return false; }
    if (now - game.away < 800 || Math.abs(want - pet.x) > 30 * K()) return false;
    game.away = 0;
    return leapTo(x, y);
  }
  // scrolled away from the pet mid-game: it hops to a perch you can see
  function keepInView(now) {
    const out = pet.y < scrollY + 70 || pet.y - 0.7 * pet.z > scrollY + innerHeight;
    if (!out) { game.outAt = 0; return false; }
    if (!game.outAt) { game.outAt = now; return false; }
    if (now - game.outAt < 600) return false;
    game.outAt = 0;
    P.run([...P.travel(P.pick() ?? P.nearest(), true), P.call(sit)]);
    return true;
  }

  // ---- yarn and feather: a toy on a string ----------------------------------
  function swing(now, dt) {
    const feather = game.mode === 'feather', k = K();
    if (!steered(now)) {
      // nobody is steering: the toy drifts around a spot on this floor
      if (game.cx == null || game.cxOn !== pet.perch) { const f = P.floor(); game.cx = clamp(pet.x, f.a + 60 * k, f.b - 60 * k); game.cxOn = pet.perch; }
      game.ax = game.cx + Math.sin(now / (feather ? 1500 : 1300)) * (feather ? 70 : 56) * k;
      game.ay = pet.y - (feather ? 190 : 130) * k + Math.sin(now / (feather ? 640 : 560)) * (feather ? 50 : 34) * k;
    } else game.cx = null;
    if (game.held) {
      // the pet has the feather in its mouth: tug it free, or wait
      const m = mouthAt();
      game.x = m.x; game.y = m.y; game.vx = game.vy = 0;
      const tug = Math.hypot(game.ax - m.x, game.ay - m.y) > 190 && steered(now);
      if (tug || now > game.held) {
        game.held = 0;
        game.vx = clamp((game.ax - m.x) * 0.06, -9, 9); game.vy = -4;
        game.busyUntil = now + 700;
        if (tug) { spark('say', m.x, m.y - 30 * k, 'Tug!'); emit('sfx', { sound: 'tug' }); }
      }
    } else {
      const L = feather ? 64 : 56, pull = feather ? 0.008 : 0.012, drag = feather ? 0.86 : 0.9;
      const flutter = feather ? Math.sin(now / 170 + Math.sin(now / 530) * 2) * 0.05 * dt : 0;
      game.vx = (game.vx + (game.ax - game.x) * pull * dt + flutter) * drag;
      game.vy = (game.vy + (game.ay + L - game.y) * pull * dt + (feather ? 0.008 : 0.02) * dt) * drag;
      game.x += game.vx; game.y += game.vy;
    }
    game.spin *= 0.94;
    const d = `M ${game.ax.toFixed(1)} ${game.ay.toFixed(1)} Q ${((game.ax + game.x) / 2 + game.vx * 2).toFixed(1)} ${((game.ay + game.y) / 2).toFixed(1)} ${game.x.toFixed(1)} ${game.y.toFixed(1)}`;
    if (feather) {
      wand.setAttribute('d', d);
      const hang = game.held ? -82 * pet.dir : -Math.atan2(game.x - game.ax, game.y - game.ay) * 57.3;
      plume.setAttribute('transform', `translate(${game.x.toFixed(1)} ${game.y.toFixed(1)}) rotate(${(hang + Math.sin(now / 120) * 10 + game.spin).toFixed(1)})`);
    } else {
      string.setAttribute('d', d);
      ball.setAttribute('transform', `translate(${game.x.toFixed(1)} ${game.y.toFixed(1)}) rotate(${((game.x * 2 + game.spin * 4) % 360).toFixed(1)})`);
    }
  }

  function knock() {
    const feather = game.mode === 'feather';
    game.vx += (feather ? 5 : 7) * pet.dir; game.vy -= feather ? 6 : 4; game.spin += (Math.random() - 0.5) * 60;
    spark('burst', game.x, game.y);
    if (feather) for (let i = 0; i < 2; i++) spark('fluff', game.x, game.y);
    // every fourth feather hit in a row ends in its mouth
    if (feather && ++game.run >= 4) {
      game.run = 0;
      game.held = performance.now() + 1700;
      score('hit', 'Caught it!', { sound: 'catch' });
    } else score('hit', 'Swat!');
  }

  // a swat in one of three heights: a paw from sitting, reared up, or a leap
  function bat(kind) {
    const R = (game.mode === 'feather' ? 30 : 26) * K();
    const s = strike(() => near(pawTip(), game.x, game.y, R), knock);
    const onMiss = s.miss(() => { const p = pawTip(); spark('whiff', p.x, p.y); score('miss', missText()); });
    if (kind === 'low') return [P.play('swat', null, (u) => { if (u > 0.2 && u < 0.62) s.check(); }), onMiss, P.call(sit)];
    if (kind === 'rear') return [P.play('rear', null, (u) => { if (u > 0.25 && u < 0.7) s.check(); }), onMiss, P.call(sit)];
    const h = clamp(pet.y - game.y - 0.62 * pet.z, 24 * K(), 200);
    return [P.hop(h, clamp(420 + h * 1.4, 460, 720), 'jump', (u, fly) => { if (fly > 0.25 && fly < 0.8) s.check(); }), onMiss, P.call(sit)];
  }

  function chase(now, dt) {
    const feather = game.mode === 'feather', k = K();
    if (game.held) { P.show('delegating'); return; }
    if (leapToward(now, game.x, game.y + 20)) return;
    const f = P.floor(), want = clamp(game.x, f.a, f.b), dx = want - pet.x;
    if (Math.abs(dx) > (feather ? 24 : 16) * k) {
      // the toy is past this floor's edge: wait at the edge for leapToward
      const far = Math.abs(dx) > 120 * k;
      stepTo(want, far ? 0.36 : 0.2, dt, far ? 'run' : 'walk');
      return;
    }
    sit();
    if (now < game.busyUntil || Math.abs(game.x - pet.x) > 64 * k) return;
    const up = pet.y - game.y;
    const z = pet.z;
    const kind = up > 0.12 * z && up < 0.5 * z ? 'low' : up >= 0.5 * z && up < 0.96 * z ? 'rear' : up >= 0.96 * z && up < 2.6 * z ? 'jump' : null;
    if (!kind) return;
    game.busyUntil = now + (feather ? 500 : 700);
    // two misses running: a quick groom, as if it meant to miss
    if (game.flops >= 2) { game.flops = 0; P.run(groom()); return; }
    pet.dir = game.x > pet.x ? 1 : -1;
    P.run(bat(kind));
  }

  // ---- laser dot: stalk, wiggle, pounce ---------------------------------------
  function shine(now, dt) {
    const k = K(), f = P.floor();
    if (!steered(now) && now > game.demoAt) {
      // nobody is steering: the dot darts somewhere on this floor, then rests
      game.demoAt = now + 1300 + Math.random() * 1400;
      game.ax = lerp(f.a, f.b, Math.random()); game.ay = pet.y - 4;
      if (Math.abs(game.ax - pet.x) < 60 * k) game.ax = clamp(pet.x + (game.ax < pet.x ? -1 : 1) * 140 * k, f.a, f.b);
    }
    const lx = game.x, ly = game.y, a = 1 - Math.exp(-dt / 45);
    game.x = lerp(game.x, game.ax, a) + (Math.random() - 0.5) * 0.8;
    game.y = lerp(game.y, game.ay, a) + (Math.random() - 0.5) * 0.8;
    game.speed = lerp(game.speed, Math.hypot(game.x - lx, game.y - ly) / Math.max(1, dt), 0.2);
    if (game.speed > 0.08) game.stillAt = now;
    // under the paws it vanishes, until you move it away
    const hidden = now < game.hideUntil && game.pin && Math.hypot(game.x - game.pin.x, game.y - game.pin.y) < 30;
    dot.setAttribute('transform', `translate(${game.x.toFixed(1)} ${game.y.toFixed(1)}) scale(${(0.92 + Math.random() * 0.16).toFixed(2)})`);
    dot.setAttribute('opacity', hidden ? 0 : 1);
  }

  function stalk(now, dt) {
    const k = K(), f = P.floor();
    const mine = game.x > f.a - 30 * k && game.x < f.b + 30 * k && game.y > pet.y - 160 * k && game.y < pet.y + 70 * k;
    if (!mine) {
      // off this floor: go to the edge nearest it, then leap toward it
      game.phase = 'watch';
      if (stepTo(game.x, 0.3, dt, 'run')) sit();
      leapToward(now, game.x, game.y - 10);
      return;
    }
    game.away = 0;
    const dx = game.x - pet.x, adx = Math.abs(dx);
    if (game.phase === 'wiggle') { wiggle(now); return; }
    if (adx > 4) pet.dir = dx > 0 ? 1 : -1;
    if (game.speed > 0.45) { game.phase = 'chase'; stepTo(game.x - 60 * k * pet.dir, 0.42, dt, 'run'); return; }
    if (adx > 260 * k) { game.phase = 'chase'; stepTo(game.x - 150 * k * pet.dir, 0.3, dt, 'run'); return; }
    // a dot it already caught has to move before it's worth another pounce
    if (game.pin && Math.hypot(game.x - game.pin.x, game.y - game.pin.y) > 40 * k) game.pin = null;
    if (game.pin) { game.phase = 'watch'; sit(); return; }
    game.phase = 'stalk';
    const still = now - game.stillAt > 260;
    if (adx > 150 * k && !still) { stepTo(game.x - 120 * k * pet.dir, 0.05, dt, 'stalk'); return; }
    P.show('stalk', 0);
    if (!still || now < game.busyUntil) return;
    if (game.flops >= 2) { game.flops = 0; P.run(groom()); return; }
    game.phase = 'wiggle';
    game.wig = { t0: now, ms: adx < 70 * k ? 420 : 620 + Math.random() * 480, x: game.x, y: game.y, lock: null };
    P.show('wiggle', null, true);
  }

  // the butt wiggle: the dot can still slip away early, but past 60% it's a commitment
  function wiggle(now) {
    const w = game.wig, k = K(), u = (now - w.t0) / w.ms;
    if (!w.lock && Math.hypot(game.x - w.x, game.y - w.y) > 45 * k) { game.phase = 'stalk'; return; }
    if (!w.lock && u > 0.6) w.lock = { x: game.x, y: game.y };
    P.show('wiggle');
    if (u >= 1) pounce(w.lock ?? { x: game.x, y: game.y });
  }

  function pounce(at) {
    game.phase = 'pounce';
    const k = K(), f = P.floor(), high = at.y < pet.y - 70 * k;
    emit('sfx', { sound: 'pounce' });
    game.busyUntil = performance.now() + 900;
    const s = strike(() => high
      ? near(pawTip(), game.x, game.y, 30 * k)
      : Math.abs(game.x - (pet.x + 0.3 * pet.z * pet.dir)) < 34 * k && game.y > pet.y - 80 * k && game.y < pet.y + 50 * k);
    const settleUp = P.call(() => {
      if (s.done) { game.pin = { x: game.x, y: game.y }; game.hideUntil = performance.now() + 1100; score('hit', 'Got it!'); }
      else score('miss', missText());
    });
    if (high) {
      const h = clamp(pet.y - at.y - 40 * k, 30, 200);
      P.run([P.hop(h, clamp(400 + h * 1.4, 440, 700), 'jump', (u, fly) => { if (fly > 0.3 && fly < 0.8) s.check(); }), settleUp, P.call(() => P.show('stalk', 0))]);
      return;
    }
    // land with the front paws where the dot was
    const lx = clamp(at.x - 0.3 * pet.z * pet.dir, f.a, f.b), ly = pet.y, lz = pet.z;
    const pinned = () => [P.act(560, () => P.show('pounce', 7)), P.call(() => {
      if (game.pin && Math.hypot(game.x - game.pin.x, game.y - game.pin.y) < 30) { const h = P.head(); spark('say', h.x, h.y - 0.26 * pet.z, '?'); }
    }), P.wait(260), P.call(() => P.show('stalk', 0))];
    const missed = () => [P.play('swat', 700), P.call(sit)];
    P.run([P.leap(() => ({ x: lx, y: ly, z: lz }), { h: clamp(Math.abs(lx - pet.x) * 0.22 + 16, 18, 64), clip: 'pounce' }), P.call(() => {
      s.check();
      P.unshift(settleUp, ...(s.done ? pinned() : missed()));
    })]);
  }

  // ---- treats: toss them, it catches them ---------------------------------------
  const G = 0.0022;
  // the ledge a treat will land on
  function landing(x, y) {
    return P.surfaceUnder(clamp(x, scrollX + 4, scrollX + innerWidth - 4), y)?.node ?? null;
  }
  function toss(x, y) {
    if (game.treats.filter((t) => !t.eaten).length >= 6) return;
    const t = { x, y, vx: (Math.random() - 0.5) * 0.12, vy: -0.34, rot: Math.random() * 360, vr: (Math.random() - 0.5) * 0.6, rest: false, eaten: false, held: false, bounces: 0, scale: 1, g: treatShape(), node: null, restAt: 0 };
    t.node = landing(x, y);
    game.treats.push(t);
    emit('sfx', { sound: 'toss' });
  }
  function eaten(t, air) {
    t.eaten = true; t.held = false;
    t.g.remove();
    for (let i = 0; i < 5; i++) spark('crumb', t.x, t.y);
    game.belly++;
    if (!air) score('snack', 'Snack', { sound: 'chomp' });
    if (game.belly >= 8) {
      // a full belly: a happy sit for a few seconds, then hungry again
      game.belly = 0;
      game.fullUntil = performance.now() + 5200;
      const h = P.head();
      spark('big', h.x, h.y - 0.34 * pet.z, 'Full!');
      spark('heart', h.x, h.y - 0.12 * pet.z);
    }
  }
  function tumble(now, dt) {
    for (const t of game.treats) {
      if (t.eaten) continue;
      if (t.held) { const m = mouthAt(); t.x = m.x; t.y = m.y; }
      else if (!t.rest) {
        t.vy += G * dt; t.x += t.vx * dt; t.y += t.vy * dt; t.rot += t.vr * dt;
        if (t.node) {
          const r = t.node.getBoundingClientRect();
          const top = P.topOf(t.node) - 4, over = t.x > r.left + scrollX && t.x < r.right + scrollX;
          if (over && t.y >= top && t.vy > 0) {
            t.y = top; t.vy *= -0.36; t.vx *= 0.55; t.vr *= 0.5; t.bounces++;
            if (t.vy > -0.07 || t.bounces > 2) { t.rest = true; t.vy = 0; t.restAt = now; }
            emit('sfx', { sound: 'tick' });
          } else if (!over && t.y > top) t.node = landing(t.x, t.y + 6);
        }
        if (t.y > scrollY + innerHeight + 40) { t.eaten = true; t.g.remove(); score('miss', 'Missed'); continue; }
      } else {
        t.y = P.topOf(t.node) - 4;
        // forgotten on a ledge too long: it quietly goes
        if (now - t.restAt > 14000) { t.eaten = true; t.g.remove(); continue; }
      }
      t.g.setAttribute('transform', `translate(${t.x.toFixed(1)} ${t.y.toFixed(1)}) rotate(${t.rest ? 0 : t.rot.toFixed(0)}) scale(${t.scale.toFixed(2)})`);
    }
    game.treats = game.treats.filter((t) => !t.eaten);
    if (game.demoAt && now > game.demoAt) { game.demoAt = 0; const h = P.head(); toss(h.x + 50 * pet.dir, h.y - 200 * K()); }
  }

  function forage(now, dt) {
    const k = K();
    if (now < game.fullUntil) { P.show('delegating'); return; }
    const live = game.treats.filter((t) => !t.held && !t.eaten);
    // in the air and coming down on this floor: get under it and jump for it
    let air = null, soon = Infinity;
    for (const t of live) {
      if (t.rest || !t.node || t.node !== pet.perch) continue;
      const dy = pet.y - 4 - t.y, tt = (t.vy + Math.sqrt(Math.max(0, t.vy * t.vy + 2 * G * dy))) / G;
      if (tt < soon) { soon = tt; air = { t, x: t.x + t.vx * tt }; }
    }
    if (air) {
      const t = air.t, m = mouthAt(), dyM = m.y - t.y;
      if (t.vy > 0 && dyM > 6 * k && dyM < 150 * k && Math.abs(t.x - m.x) < 30 * k && now > game.busyUntil) {
        game.busyUntil = now + 500;
        const h = clamp(dyM - 0.1 * pet.z, 16, 150);
        const s = strike(() => near(mouthAt(), t.x, t.y, 24 * k), () => { t.held = true; score('hit', 'Chomp!', { sound: 'chomp' }); });
        P.run([P.hop(h, clamp(340 + h * 1.6, 380, 640), 'jump', (u) => s.check()), P.call(() => {
          if (s.done) P.unshift(P.play('eat', 520), P.call(() => eaten(t, true)), P.call(sit));
        })]);
        return;
      }
      // stand so the mouth, not the paws, ends up under it
      const side = air.x >= pet.x ? 1 : -1, tx = air.x - side * Math.abs(mouthAt().x - pet.x);
      if (Math.abs(tx - pet.x) > 6) stepTo(tx, 0.45, dt, 'run');
      else { pet.dir = side; P.show('jump', 0); }
      return;
    }
    // resting: walk over and eat it, or leap to the ledge it landed on
    let rest = null, rd = Infinity;
    for (const t of live) if (t.rest) { const d = Math.abs(t.x - pet.x) + (t.node === pet.perch ? 0 : 1e4); if (d < rd) { rd = d; rest = t; } }
    if (rest && rest.node !== pet.perch) { leapTo(rest.x, rest.y - 20, rest.node); return; }
    if (rest) {
      const side = rest.x >= pet.x ? 1 : -1;
      if (!stepTo(rest.x - side * 0.05 * pet.z, 0.26, dt)) return;
      pet.dir = side;
      // the eat clip draws its own treat, so the tossed one goes as it starts
      P.run([P.call(() => { rest.scale = 0; }), P.play('eat'), P.call(() => eaten(rest, false)), P.call(sit)]);
      return;
    }
    sit();
  }

  // ---- start, switch and stop -----------------------------------------------------
  function hideToy() {
    if (!game.mode) return;
    toyG[game.mode].setAttribute('display', 'none');
    for (const t of game.treats) t.g.remove();
    game.treats = []; game.held = 0;
  }
  function setPlay(mode) {
    const was = game.mode, now = performance.now();
    hideToy();
    Object.assign(game, {
      mode, phase: 'watch', wig: null, pin: null, hideUntil: 0, held: 0, spin: 0,
      hits: 0, misses: 0, streak: 0, best: 0, flops: 0, run: 0, busyUntil: now + 600, away: 0, outAt: 0,
      belly: 0, fullUntil: 0, demoAt: mode === 'treats' ? now + 1800 : 0, auto: true, t: now, speed: 0, stillAt: now,
    });
    toyG[mode].setAttribute('display', 'inline');
    P.game = tick;
    if (!was) {
      // out of the seal and onto the floor under it, or ready where it sits
      if (!pet.away) P.run([...P.travel(P.stage, true), P.call(sit), P.call(placeToy)]);
      else P.run([P.play('rear'), P.call(sit), P.call(placeToy)]);
    } else placeToy();
    emit('start');
  }
  function placeToy() {
    const h = P.head();
    game.ax = h.x + 70 * pet.dir; game.ay = h.y - 90; game.x = game.ax; game.y = game.ay + 50; game.vx = game.vy = 0;
    if (game.mode === 'laser') { game.x = game.ax = pet.x + 140 * K() * pet.dir; game.y = game.ay = pet.y - 4; }
  }
  function stopPlay() {
    if (!game.mode) return;
    hideToy();
    game.mode = null; game.phase = 'watch';
    P.game = null;
    if (pet.perch) { const r = pet.perch.getBoundingClientRect(); pet.frac = clamp((pet.x - r.left - scrollX) / r.width, 0, 1); }
    P.run([P.play('delivered'), P.rest('delegating')]);
    P.refollow();
    emit('stop');
  }

  // ---- the loop ----------------------------------------------------------------
  function tick(now, dt) {
    const m = game.mode;
    if (m === 'laser') shine(now, dt);
    else if (m === 'treats') tumble(now, dt);
    else swing(now, dt);
    if (P.busy() || !pet.away) return;
    if (pet.perch) pet.y = P.spot(pet.perch).y;
    if (keepInView(now)) return;
    if (m === 'laser') stalk(now, dt);
    else if (m === 'treats') forage(now, dt);
    else chase(now, dt);
  }
  P.after = (now) => drawSparks(now);
  // tapping the hero cat while playing counts as a pet: a heart
  P.onHomeTap = () => { const s = P.home.getBoundingClientRect(); spark('heart', s.left + scrollX + s.width / 2, s.top + scrollY + s.height * 0.3); };

  return {
    play: setPlay, stop: stopPlay,
    get playing() { return game.mode; },
  };
}

// Tiny synthesized blips. Off by default; the audio context starts only when a
// visitor turns sound on, so it always follows a gesture.
export function makeSound() {
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
