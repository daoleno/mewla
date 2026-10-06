import { createCat } from './cat-rig.js';

const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
const FULL = [106, 128, 440, 286];

const TILES = [
  { title: '呼吸', text: '吸气快、呼气慢、中间停一下，每一口都略有不同。只有背和肋部起伏。', view: FULL, mode: 'breath' },
  { title: '抖耳朵', text: '单边快速向后一弹再回弹，有时连抖两下。', view: [140, 148, 196, 148], clip: 'flick', gap: 1.2 },
  { title: '尾巴尖', text: '尾尖抬起、卷一下、轻轻甩一次，再慢慢放下。', view: [236, 274, 220, 165], clip: 'tail' },
  { title: '做梦', text: '前爪抽动、胡须和鼻子一颤，闭着的眼皮跟着动。', view: [138, 214, 214, 156], clip: 'dream' },
  { title: '偷看', text: '一只眼睛眯开一条缝，看一眼，慢慢眨一下，再合上。', view: [186, 240, 112, 76], clip: 'peek', gap: 1.4 },
  { title: '醒来 · 哈欠', text: '耳朵竖起、抬头、睡眼惺忪、慢眨、大哈欠、叹口气重新躺下。', view: FULL, clip: 'wake', gap: 2 },
  { title: '毛发特写', text: '静态放大：明暗、条纹、边缘的毛和颗粒，检查质感。', view: [372, 182, 104, 78], mode: 'static' },
];

const cats = [];
const hero = document.getElementById('hero');
const main = createCat(hero, { view: FULL, mode: 'auto', interactive: true, still, seed: 5 });
cats.push(main);
window.zenCats = cats;
document.getElementById('wake').addEventListener('click', () => main.wake());

const box = document.getElementById('tiles');
TILES.forEach((t, i) => {
  const el = document.createElement('article');
  el.className = 'tile';
  el.innerHTML = `<canvas aria-label="${t.title}"></canvas><div class="meta"><h3>${t.title}</h3><p>${t.text}</p>${t.clip ? '<button type="button" class="btn">重播</button>' : ''}</div>`;
  box.appendChild(el);
  const cat = createCat(el.querySelector('canvas'), { view: t.view, mode: t.mode || 'only', clip: t.clip, gap: t.gap ?? 1.6, still, seed: 20 + i });
  cats.push(cat);
  const btn = el.querySelector('.btn');
  if (btn) btn.addEventListener('click', () => cat.replay());
});

for (const b of document.querySelectorAll('[data-speed]')) {
  b.addEventListener('click', () => {
    for (const x of document.querySelectorAll('[data-speed]')) x.setAttribute('aria-pressed', String(x === b));
    cats.forEach((c) => c.setSpeed(Number(b.dataset.speed)));
  });
}
