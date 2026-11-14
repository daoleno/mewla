import { signal } from "nsh";

/**
 * The one clock every elapsed time on screen reads (SPEC §11 Elapsed time): the Working line, tray rows,
 * Work cards and Work detail rows tick together, so the same turn never shows two times at once.
 */
export const elapsedNow = signal(Date.now());
setInterval(() => elapsedNow.set(Date.now()), 1000);
