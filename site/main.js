// Mewla landing: the pet, the links and the small bits.
import { startPet } from './pet.js';

// Every URL that depends on where Mewla's source and builds are published, in one
// place. An empty value hides every link (and the install command) that uses it.
const LINKS = {
  source: 'https://github.com/daoleno/mewla',
  releases: 'https://github.com/daoleno/mewla/releases',
  apk: 'https://github.com/daoleno/mewla/releases/latest',
  installScript: 'https://raw.githubusercontent.com/daoleno/mewla/main/install.sh',
};
for (const node of document.querySelectorAll('[data-link]')) {
  const url = LINKS[node.dataset.link];
  node.hidden = !url;
  if (!url) continue;
  if (node.tagName === 'A') node.href = url;
  for (const code of node.querySelectorAll('[data-cmd]')) code.textContent = code.dataset.cmd.replace('{}', url);
}

startPet(document.querySelector('[data-pet="home"]'), document.querySelector('[data-pet="strip"]'));

// the works-with strip loops: a second, hidden copy follows the first
const row = document.querySelector('.works .row');
const twin = row.cloneNode(true);
twin.setAttribute('aria-hidden', 'true');
row.after(twin);

// illustrations play once, as they come into view
const shown = document.querySelectorAll('.rv');
if ('IntersectionObserver' in window) {
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target); }
  }, { rootMargin: '0px 0px -12% 0px' });
  shown.forEach((n) => io.observe(n));
} else shown.forEach((n) => n.classList.add('in'));

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
