import { host, platform, signal } from "nsh";
import { platform as corePlatform } from "../../core/platform";
import { importHandover, parseHandover } from "../../core/update/handover";

/**
 * Android, the new app of a move (a release of another application id, client/core/update/handover.ts): at the
 * start, before the app reads its computers, the pairing the old app staged is taken once into this fresh install,
 * and the old app is told to forget it. Host calls: `handover.take` → the staged JSON or null, `handover.taken`,
 * `handover.legacyInstalled` → boolean, `handover.removeLegacy` (the system's uninstall dialog for the old app).
 * Each fails where the build has none (the Play build, iOS, the web): nothing then, and the person scans.
 */
export async function takeHandover(): Promise<void> {
  if (platform.os !== "android" || !host.available()) return;
  try {
    const staged = parseHandover(await host.call("handover.take"));
    if (!staged) return;
    const result = await importHandover(staged, corePlatform().secure, corePlatform().storage);
    console.warn(`handover: ${result}`);
    await host.call("handover.taken");
  } catch (error) {
    console.warn("handover failed", error instanceof Error ? error.message : error);
  }
}

/** Whether the old Mewla (the id before the move) is still installed beside this one. */
export const oldAppInstalled = signal(false);

export async function checkOldApp(): Promise<void> {
  if (platform.os !== "android" || !host.available()) return;
  oldAppInstalled.set((await host.call("handover.legacyInstalled").catch(() => false)) === true);
}

export async function removeOldApp(): Promise<void> {
  await host.call("handover.removeLegacy").catch(() => undefined);
}
