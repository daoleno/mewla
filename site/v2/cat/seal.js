// The seal cat demo: the mini header seal, the hero seal and the hopping cat.
import { buildSeal, startCat } from '../shared/sealcat.js';

buildSeal(document.querySelector('[data-seal="mini"]'), { detail: false });
const sealSvg = document.querySelector('[data-seal="hero"]');
buildSeal(sealSvg, { ghost: true });
window.zenSeal = startCat(sealSvg);
