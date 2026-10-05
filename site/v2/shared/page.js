// Shared page behaviour. Every element is styled in its finished state; motion
// classes only replay it: .arm resets to the first frame, .play runs it.
import { reveal } from "./motion.js";

for (const el of document.querySelectorAll("[data-reveal]")) {
  reveal(el, {
    arm: () => el.classList.add("arm"),
    play: () => requestAnimationFrame(() => requestAnimationFrame(() => el.classList.add("play"))),
  });
}
