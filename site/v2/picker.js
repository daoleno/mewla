import { catSVG } from './shared/cat.js';

// A small parade at the top of the chooser, so the mascot library is visible
// before you open any direction.
const row = document.getElementById('cat-row');
if (row) {
  row.innerHTML = ['sleep', 'loaf', 'sit', 'stretch', 'walk', 'knead', 'peek', 'pounce']
    .map((p) => catSVG(p))
    .join('');
}
