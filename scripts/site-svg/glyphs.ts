// Prints the app's icon set (client/ui/icons/glyphs.ts) for the
// README drawings. Output: JSON {name: {strokes: [d], fills?: [d]}} on the 24 grid.
// Run: bun scripts/site-svg/glyphs.ts
import { MEWLA_GLYPHS } from "../../client/ui/icons/glyphs";

console.log(JSON.stringify(MEWLA_GLYPHS));
