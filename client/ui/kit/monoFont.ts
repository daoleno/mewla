import regular from "../assets/fonts/MapleMono-NF-Regular.ttf";
import semibold from "../assets/fonts/MapleMono-NF-SemiBold.ttf";

/**
 * The face of the theme's `font-mono` (theme.json, first in its list): the terminal's Maple Mono NF
 * (SIL OFL 1.1, assets/fonts/OFL.txt), Regular without the icons and SemiBold, made by
 * scripts/subset-terminal-font.py --ui. Importing it registers both before the first frame, so inline
 * code, code blocks, commands and diffs are monospaced on every platform, not in whatever a phone's
 * vendor maps to `monospace`.
 */
export const MONO_FAMILY: string = regular;

/** Both files name the one family. */
export const MONO_FACES: readonly string[] = [regular, semibold];
