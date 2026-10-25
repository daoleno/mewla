(function () {
'use strict';
/* The cat's few sounds, made in the page itself: no audio files, nothing to
   download. Silent until the first touch (a browser starts audio only inside a
   gesture) and never louder than a room at night. There is no switch: the page
   stops its sounds when it is not on screen. */

var ctx = null;
var master = null;
var tone = null;            // the room tone, started with the first touch
var night = false;
var paused = false;
var cricketTimer = 0;

function make() {
  var AC = window.AudioContext || window.webkitAudioContext;
  if (!AC) return;
  try { ctx = new AC(); } catch (e) { ctx = null; return; }
  master = ctx.createGain();
  master.gain.value = 0.9;
  master.connect(ctx.destination);
}

function noise(seconds) {
  var n = Math.floor(ctx.sampleRate * seconds), buf = ctx.createBuffer(1, n, ctx.sampleRate), d = buf.getChannelData(0), last = 0;
  for (var i = 0; i < n; i++) { last = (last + (Math.random() * 2 - 1) * 0.06) / 1.02; d[i] = last * 3.2; }   // soft brown noise
  return buf;
}

function startTone() {
  if (tone || !ctx) return;
  var src = ctx.createBufferSource(); src.buffer = noise(4); src.loop = true;
  var low = ctx.createBiquadFilter(); low.type = 'lowpass'; low.frequency.value = 420;
  var gain = ctx.createGain(); gain.gain.value = 0;
  src.connect(low); low.connect(gain); gain.connect(master); src.start();
  gain.gain.linearRampToValueAtTime(0.05, ctx.currentTime + 3);
  tone = { src: src, gain: gain };
  crickets();
}

/* a cricket now and then, only at night */
function crickets() {
  clearTimeout(cricketTimer);
  cricketTimer = setTimeout(function () {
    if (ctx && night && !paused && ctx.state === 'running') {
      var t = ctx.currentTime, osc = ctx.createOscillator(), g = ctx.createGain();
      osc.type = 'sine'; osc.frequency.value = 4300 + Math.random() * 300;
      g.gain.value = 0;
      for (var i = 0; i < 3; i++) { g.gain.setValueAtTime(0, t + i * 0.14); g.gain.linearRampToValueAtTime(0.012, t + i * 0.14 + 0.03); g.gain.linearRampToValueAtTime(0, t + i * 0.14 + 0.1); }
      osc.connect(g); g.connect(master); osc.start(t); osc.stop(t + 0.5);
    }
    crickets();
  }, 5000 + Math.random() * 9000);
}

function blip(freq, to, ms, vol, type) {
  if (!ctx || paused || ctx.state !== 'running') return;
  var t = ctx.currentTime, o = ctx.createOscillator(), g = ctx.createGain();
  o.type = type || 'sine'; o.frequency.setValueAtTime(freq, t); o.frequency.exponentialRampToValueAtTime(to, t + ms / 1000);
  g.gain.setValueAtTime(0, t); g.gain.linearRampToValueAtTime(vol, t + 0.012); g.gain.exponentialRampToValueAtTime(0.0001, t + ms / 1000);
  o.connect(g); g.connect(master); o.start(t); o.stop(t + ms / 1000 + 0.05);
}

/* a short filtered hiss: paper, cloth, a sizzle, a crunch */
function burst(seconds, freq, vol) {
  if (!ctx || paused || ctx.state !== 'running') return;
  var t = ctx.currentTime, src = ctx.createBufferSource(); src.buffer = noise(seconds + 0.05);
  var f = ctx.createBiquadFilter(); f.type = 'bandpass'; f.frequency.value = freq; f.Q.value = 0.8;
  var g = ctx.createGain(); g.gain.setValueAtTime(0, t); g.gain.linearRampToValueAtTime(vol, t + 0.02); g.gain.linearRampToValueAtTime(0, t + seconds);
  src.connect(f); f.connect(g); g.connect(master); src.start(t); src.stop(t + seconds + 0.05);
}

var Sound = {
  /* called from every touch; the first one wakes the page's audio */
  unlock: function () {
    if (!ctx) make();
    if (!ctx) return;
    if (ctx.state === 'suspended' && !paused) ctx.resume();
    if (!tone) startTone();
  },
  pause: function (on) {
    paused = !!on;
    if (!ctx) return;
    if (paused) ctx.suspend(); else if (tone) ctx.resume();
  },
  setNight: function (on) { night = !!on; if (tone && ctx) tone.gain.gain.linearRampToValueAtTime(night ? 0.03 : 0.05, ctx.currentTime + 4); },
  purr: function () {
    if (!ctx || paused || ctx.state !== 'running') return;
    var t = ctx.currentTime, src = ctx.createBufferSource(); src.buffer = noise(2);
    var band = ctx.createBiquadFilter(); band.type = 'bandpass'; band.frequency.value = 110; band.Q.value = 1.4;
    var am = ctx.createGain(); am.gain.value = 0.5;
    var lfo = ctx.createOscillator(); lfo.frequency.value = 24; var depth = ctx.createGain(); depth.gain.value = 0.5;
    lfo.connect(depth); depth.connect(am.gain);
    var env = ctx.createGain(); env.gain.setValueAtTime(0, t); env.gain.linearRampToValueAtTime(0.55, t + 0.25); env.gain.linearRampToValueAtTime(0.4, t + 1.1); env.gain.linearRampToValueAtTime(0, t + 1.9);
    src.connect(band); band.connect(am); am.connect(env); env.connect(master);
    src.start(t); lfo.start(t); src.stop(t + 2); lfo.stop(t + 2);
  },
  paper: function () {
    if (!ctx || paused || ctx.state !== 'running') return;
    var t = ctx.currentTime, src = ctx.createBufferSource(); src.buffer = noise(0.5);
    var hi = ctx.createBiquadFilter(); hi.type = 'highpass'; hi.frequency.value = 1800;
    var g = ctx.createGain(); g.gain.setValueAtTime(0, t);
    [0, 0.07, 0.15, 0.22].forEach(function (d, i) { g.gain.linearRampToValueAtTime(0.16 - i * 0.025, t + d + 0.01); g.gain.linearRampToValueAtTime(0.02, t + d + 0.06); });
    g.gain.linearRampToValueAtTime(0, t + 0.45);
    src.connect(hi); hi.connect(g); g.connect(master); src.start(t); src.stop(t + 0.5);
  },
  pop: function () { blip(520, 980, 150, 0.12, 'sine'); },
  chime: function () { blip(880, 1320, 420, 0.1, 'sine'); setTimeout(function () { blip(1320, 1760, 520, 0.07, 'sine'); }, 110); },
  thump: function () { blip(150, 70, 110, 0.14, 'sine'); },
  chirp: function () { blip(2400, 3300, 90, 0.05, 'sine'); setTimeout(function () { blip(2900, 2300, 120, 0.04, 'sine'); }, 130); },
  clink: function () { blip(1650, 2100, 260, 0.07, 'triangle'); setTimeout(function () { blip(2480, 2600, 300, 0.05, 'sine'); }, 70); },
  munch: function () { [0, 160, 330].forEach(function (d) { setTimeout(function () { burst(0.06, 900, 0.12); }, d); }); },
  wood: function () { blip(240, 180, 120, 0.13, 'triangle'); setTimeout(function () { blip(330, 260, 90, 0.08, 'triangle'); }, 90); },
  cloth: function () { burst(0.22, 2400, 0.07); },
  plop: function () { blip(700, 180, 220, 0.12, 'sine'); setTimeout(function () { blip(1200, 600, 120, 0.04, 'sine'); }, 120); },
  /* a small meow, made from a sliding tone with a soft vowel */
  meow: function (v) {
    if (!ctx || paused || ctx.state !== 'running') return;
    var t = ctx.currentTime, o = ctx.createOscillator(), f = ctx.createBiquadFilter(), g = ctx.createGain(), vol = 0.07 * (v || 1);
    o.type = 'sawtooth'; o.frequency.setValueAtTime(520, t); o.frequency.linearRampToValueAtTime(760, t + 0.16); o.frequency.linearRampToValueAtTime(480, t + 0.5);
    f.type = 'bandpass'; f.frequency.setValueAtTime(900, t); f.frequency.linearRampToValueAtTime(1500, t + 0.2); f.Q.value = 3;
    g.gain.setValueAtTime(0, t); g.gain.linearRampToValueAtTime(vol, t + 0.05); g.gain.linearRampToValueAtTime(vol * 0.8, t + 0.3); g.gain.linearRampToValueAtTime(0, t + 0.55);
    o.connect(f); f.connect(g); g.connect(master); o.start(t); o.stop(t + 0.6);
  },
  /* each hobby has its own little sound */
  hobby: function (id) {
    var notes = { music: [523, 659, 784, 659], letters: null, paint: null, cook: null, tinker: [1800, 2200, 1800], fish: null, paper: null, garden: null, stargaze: [880, 1175, 1568], camp: null }[id];
    if (notes) notes.forEach(function (n, i) { setTimeout(function () { blip(n, n * 1.01, id === 'music' ? 520 : 160, id === 'music' ? 0.06 : 0.04, id === 'music' ? 'triangle' : 'sine'); }, i * (id === 'music' ? 190 : 120)); });
    else if (id === 'cook') burst(1.2, 3200, 0.04);
    else if (id === 'paint' || id === 'letters') [0, 220, 470].forEach(function (d) { setTimeout(function () { burst(0.18, 3800, 0.05); }, d); });
    else if (id === 'paper') burst(0.35, 2600, 0.08);
    else if (id === 'garden') [0, 90, 170, 260].forEach(function (d) { setTimeout(function () { blip(1400 + Math.random() * 600, 900, 70, 0.04, 'sine'); }, d); });
  },
  get state() { return ctx ? ctx.state : 'none'; }
};
window.CatSound = Sound;
})();
