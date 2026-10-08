// Prints the app's icon set (app/components/icons/mewlaGlyphs.ts) for the
// README drawings. Output: JSON {name: {strokes: [d], fills?: [d]}} on the 24 grid.
// Run: bun scripts/site-svg/glyphs.ts
import { MEWLA_GLYPHS } from "../../app/components/icons/mewlaGlyphs";

console.log(JSON.stringify(MEWLA_GLYPHS));
