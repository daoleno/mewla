import * as THREE from 'https://cdn.jsdelivr.net/npm/three@0.170.0/build/three.module.js';
import { catSVG, zzz } from '../shared/cat.js';

// ---- 2D mascot inside the page -------------------------------------------
for (const slot of document.querySelectorAll('.cat-slot')) {
  slot.insertAdjacentHTML('afterbegin', catSVG(slot.dataset.cat || 'sleep'));
}
const endZ = document.querySelector('.end .cat-slot');
if (endZ) {
  const box = document.createElement('div');
  box.className = 'zzz';
  box.innerHTML = zzz({ n: 3, x: 58, y: 18 });
  endZ.appendChild(box);
}

// ---- shared page behaviour ----------------------------------------------
const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
const bar = document.querySelector('.bar');
const onScroll = () => bar.classList.toggle('scrolled', scrollY > 8);
addEventListener('scroll', onScroll, { passive: true });
onScroll();

const targets = [...document.querySelectorAll('.cards li, .three li, .col-title, .kit .phone, .beat, .yours-grid > *, .end-copy > *')];
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
      btn.textContent = 'Failed';
    }
    setTimeout(() => { btn.textContent = 'Copy'; }, 1600);
  });
}

// ---- the 3D cat ----------------------------------------------------------
const canvas = document.getElementById('cat3d');
const hint = document.getElementById('cat-hint');
const stage = canvas && canvas.closest('.cat-stage');
const tag = stage && stage.querySelector('.stage-tag');

if (canvas) {
  const FUR = 0xf2a95c;
  const FUR_DARK = 0xdf8a3c;
  const CREAM = 0xfbe0bd;
  const INK = 0x3d2b21;
  const BLUSH = 0xef9e9e;

  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true, powerPreference: 'high-performance' });
  renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  renderer.shadowMap.enabled = true;
  renderer.shadowMap.type = THREE.PCFSoftShadowMap;

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(34, 1, 0.1, 100);

  // Lights: a soft key, a cool fill and a rim, so the fur reads rounded.
  scene.add(new THREE.HemisphereLight(0xffffff, 0xdfe6ff, 1.5));
  const key = new THREE.DirectionalLight(0xfff2dd, 2.1);
  key.position.set(3.4, 5.2, 3.6);
  key.castShadow = true;
  key.shadow.mapSize.set(1024, 1024);
  key.shadow.radius = 4;
  key.shadow.camera.near = 0.5;
  key.shadow.camera.far = 22;
  key.shadow.camera.left = -6; key.shadow.camera.right = 6;
  key.shadow.camera.top = 6; key.shadow.camera.bottom = -6;
  scene.add(key);
  const rim = new THREE.DirectionalLight(0x9fc4ff, 1.1);
  rim.position.set(-4, 2.4, -3.4);
  scene.add(rim);

  // Materials, all flat-shaded so the facets show.
  const mat = (color, extra = {}) => new THREE.MeshStandardMaterial({ color, roughness: .82, metalness: 0, flatShading: true, ...extra });
  const fur = mat(FUR);
  const furDark = mat(FUR_DARK);
  const cream = mat(CREAM);
  const ink = mat(INK, { roughness: .55 });
  const blush = mat(BLUSH, { roughness: .9 });
  const pink = mat(0xe98b8b);

  const cat = new THREE.Group();
  const geo = new THREE.SphereGeometry(1, 20, 14);

  // body: a squashed sphere
  const body = new THREE.Mesh(geo, fur);
  body.scale.set(1.62, 0.92, 1.16);
  body.position.set(0.34, 0.94, 0);
  body.castShadow = true;
  cat.add(body);

  // head
  const head = new THREE.Mesh(geo, fur);
  head.scale.setScalar(0.86);
  head.position.set(-1.42, 1.03, 0);
  head.castShadow = true;
  cat.add(head);

  // ears: two cones on top of the head, tipped outward
  const earGeo = new THREE.ConeGeometry(0.34, 0.66, 4);
  const innerGeo = new THREE.ConeGeometry(0.19, 0.42, 4);
  for (const side of [1, -1]) {
    const ear = new THREE.Mesh(earGeo, fur);
    ear.position.set(-1.5, 1.8, 0.44 * side);
    ear.rotation.set(0.38 * side, 0, 0.12);
    ear.castShadow = true;
    cat.add(ear);
    const inner = new THREE.Mesh(innerGeo, cream);
    inner.position.set(-1.63, 1.76, 0.42 * side);
    inner.rotation.copy(ear.rotation);
    cat.add(inner);
  }

  // closed eyes: two flattened tori halves, plus a tiny nose and a smile
  const eyeGeo = new THREE.TorusGeometry(0.14, 0.038, 6, 12, Math.PI);
  for (const z of [0.3, -0.3]) {
    const eye = new THREE.Mesh(eyeGeo, ink);
    eye.position.set(-2.2, 1.1, z);
    eye.rotation.set(0, Math.PI / 2, Math.PI);
    cat.add(eye);
    const cheek = new THREE.Mesh(geo, blush);
    cheek.scale.set(0.09, 0.05, 0.11);
    cheek.position.set(-2.02, 0.9, z * 1.7);
    cat.add(cheek);
  }
  const nose = new THREE.Mesh(new THREE.ConeGeometry(0.075, 0.1, 3), pink);
  nose.position.set(-2.27, 0.95, 0);
  nose.rotation.set(0, 0, -Math.PI / 2);
  cat.add(nose);

  // stripes on the back
  for (const [x, s] of [[0.12, 1], [0.58, 1.06], [1.04, 1.1]]) {
    const stripe = new THREE.Mesh(new THREE.TorusGeometry(0.62 * s, 0.055, 5, 10, Math.PI), furDark);
    stripe.position.set(x, 0.98, 0);
    stripe.rotation.set(Math.PI / 2, 0, Math.PI / 2);
    cat.add(stripe);
  }

  // front paws, tucked under the chin
  for (const z of [0.36, -0.36]) {
    const paw = new THREE.Mesh(geo, cream);
    paw.scale.set(0.34, 0.2, 0.26);
    paw.position.set(-1.34, 0.22, z);
    paw.castShadow = true;
    cat.add(paw);
  }

  // tail: a swept tube that curls around the body
  const tailCurve = new THREE.CatmullRomCurve3([
    new THREE.Vector3(1.72, 0.86, 0.1),
    new THREE.Vector3(2.2, 0.6, 0.5),
    new THREE.Vector3(2.1, 0.34, 1.0),
    new THREE.Vector3(1.2, 0.26, 1.2),
    new THREE.Vector3(0.2, 0.24, 0.9),
  ]);
  const tail = new THREE.Mesh(new THREE.TubeGeometry(tailCurve, 26, 0.19, 7, false), fur);
  tail.castShadow = true;
  cat.add(tail);
  const tip = new THREE.Mesh(geo, furDark);
  tip.scale.setScalar(0.2);
  tip.position.copy(tailCurve.getPoint(1));
  cat.add(tip);

  // ground: a soft elliptical shelf with a real contact shadow
  const shelf = new THREE.Mesh(new THREE.CircleGeometry(3.4, 48), new THREE.MeshStandardMaterial({ color: 0xffffff, roughness: 1, metalness: 0, transparent: true, opacity: .0 }));
  shelf.rotation.x = -Math.PI / 2;
  shelf.receiveShadow = true;
  scene.add(shelf);

  const shadowPlane = new THREE.Mesh(new THREE.CircleGeometry(2.2, 40), new THREE.MeshBasicMaterial({ color: 0x6f86d6, transparent: true, opacity: .16 }));
  shadowPlane.rotation.x = -Math.PI / 2;
  shadowPlane.position.set(0.2, 0.012, 0);
  scene.add(shadowPlane);

  cat.position.y = 0.06;
  // turn the face toward the default camera
  cat.rotation.y = 0.9;
  scene.add(cat);

  // ---- camera + interaction --------------------------------------------
  const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
  const view = { yaw: -0.3, pitch: 0.30, dist: 8.2, target: new THREE.Vector3(0.35, 1.02, 0) };
  const want = { yaw: view.yaw, pitch: view.pitch };
  const start = { x: 0, y: 0 };
  let dragging = false;
  let idle = 0;

  function place() {
    const { yaw, pitch, dist, target } = view;
    camera.position.set(
      target.x + dist * Math.cos(pitch) * Math.sin(yaw),
      target.y + dist * Math.sin(pitch),
      target.z + dist * Math.cos(pitch) * Math.cos(yaw),
    );
    camera.lookAt(target);
  }

  function resize() {
    const w = canvas.clientWidth || 480;
    const h = canvas.clientHeight || 400;
    renderer.setSize(w, h, false);
    camera.aspect = w / h;
    // frame the whole cat, with a little air around it
    camera.fov = 38;
    camera.updateProjectionMatrix();
    view.dist = 8.2;
  }
  addEventListener('resize', resize);

  const surface = stage || canvas;
  surface.addEventListener('pointerdown', (e) => {
    dragging = true; idle = 0; start.x = e.clientX; start.y = e.clientY;
    surface.setPointerCapture(e.pointerId);
    cat.userData.wake = 1.0;
  });
  surface.addEventListener('pointermove', (e) => {
    if (!dragging) return;
    want.yaw = clamp(want.yaw - (e.clientX - start.x) * 0.005, -0.9, 0.62);
    want.pitch = clamp(want.pitch + (e.clientY - start.y) * 0.004, 0.02, 0.62);
    start.x = e.clientX; start.y = e.clientY;
    idle = 0;
    if (still) requestAnimationFrame(render);
  });
  const stopDrag = () => { dragging = false; if (still) requestAnimationFrame(render); };
  surface.addEventListener('pointerup', stopDrag);
  surface.addEventListener('pointercancel', stopDrag);
  surface.addEventListener('lostpointercapture', stopDrag);

  // The tag flips to "awake" while you are handling the cat.

  let glow = 0;
  const clock = new THREE.Clock();

  // Reveal the canvas once the first frame is drawn.
  let shown = false;

  function render() {
    // getDelta first: getElapsedTime would reset the delta to ~0
    const dt = Math.min(clock.getDelta(), 0.05);
    const t = clock.elapsedTime;

    // ease the camera
    // with reduced motion the camera jumps straight to where you dragged it
    const ease = still ? 1 : 0.09;
    view.yaw += (want.yaw - view.yaw) * ease;
    view.pitch += (want.pitch - view.pitch) * ease;
    place();

    // breathing: the body and head rise a hair, the tail flicks
    const breath = still ? 0 : Math.sin(t * 0.9) * 0.022;
    body.scale.set(1.62 + breath * 0.6, 0.92 + breath, 1.16 + breath * 0.4);
    body.position.y = 0.94 + breath * 0.4;
    head.position.y = 1.03 + breath * 0.6;
    tail.rotation.y = still ? 0 : Math.sin(t * 0.7) * 0.05;

    // eyes open a crack when the cat is "awake", then close again
    const wake = cat.userData.wake || 0;
    if (wake > 0) cat.userData.wake = Math.max(0, wake - dt * 0.5);
    const open = Math.min(1, (cat.userData.wake || 0) * 3);
    for (const e of cat.children) {
      if (e.geometry && e.geometry.type === 'TorusGeometry' && e.material === ink) {
        e.scale.y = 1 + open * 0.6;
        e.rotation.z = Math.PI + open * 0.5;
      }
    }

    // a slow colour shift: warmer when you interact
    glow += ((dragging ? 1 : 0) - glow) * 0.05;
    fur.color.setHex(FUR).lerp(new THREE.Color(0xffb972), glow * 0.5);
    key.intensity = 2.1 + glow * 0.5;

    if (!still) idle += dt;
    // drift back to the home angle after a while untouched
    if (!dragging && idle > 6) {
      want.yaw += (-0.3 - want.yaw) * 0.02;
      want.pitch += (0.30 - want.pitch) * 0.02;
    }

    renderer.render(scene, camera);
    if (tag) {
      const awake = dragging || (cat.userData.wake || 0) > 0.05;
      tag.classList.toggle('awake', awake);
      tag.lastChild.textContent = awake ? 'awake' : 'asleep';
    }
    if (!shown) { shown = true; canvas.classList.add('on'); hint && hint.classList.add('on'); }
  }


  resize();
  place();
  if (still) {
    // one still frame: nothing moves, so nothing needs a loop
    render();
  } else {
    // Pause the loop when the hero scrolls away.
    const vis = new IntersectionObserver(([e]) => {
      if (!e.isIntersecting) { renderer.setAnimationLoop(null); return; }
      renderer.setAnimationLoop(render);
    }, { rootMargin: '120px' });
    renderer.setAnimationLoop(render);
    vis.observe(canvas);
  }
}
