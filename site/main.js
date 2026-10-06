// Zen landing: the seals, the hopping cat and the small bits.
import { buildSeal, startCat } from './sealcat.js';

buildSeal(document.querySelector('[data-seal="mini"]'), { detail: false });
const sealSvg = document.querySelector('[data-seal="hero"]');
buildSeal(sealSvg, { ghost: true });
startCat(sealSvg);

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
