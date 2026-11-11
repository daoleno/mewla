/**
 * Mewla's versions: SemVer precedence as the computer's self-update compares
 * (daemon/selfupdate.compareSemVersion), and the oldest computer this app works with.
 */

interface SemVersion {
  core: [number, number, number];
  prerelease: string[];
}

export function parseVersion(raw: string): SemVersion | null {
  const value = raw.trim().replace(/^v/, "").split("+", 1)[0];
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(.+))?$/.exec(value);
  if (!match) return null;
  const prerelease = match[4] === undefined ? [] : match[4].split(".");
  for (const part of prerelease) {
    if (!/^[0-9A-Za-z-]+$/.test(part) || (/^\d+$/.test(part) && part.length > 1 && part[0] === "0")) {
      return null;
    }
  }
  const core = [Number(match[1]), Number(match[2]), Number(match[3])] as [number, number, number];
  if (!core.every(Number.isSafeInteger)) return null;
  return { core, prerelease };
}

/** SemVer precedence, matching daemon/selfupdate.compareSemVersion; null when either is no version. */
export function compareVersions(left: string, right: string): number | null {
  const a = parseVersion(left);
  const b = parseVersion(right);
  if (!a || !b) return null;
  for (let index = 0; index < 3; index += 1) {
    if (a.core[index] !== b.core[index]) return a.core[index] < b.core[index] ? -1 : 1;
  }
  if (!a.prerelease.length || !b.prerelease.length) {
    return a.prerelease.length === b.prerelease.length ? 0 : a.prerelease.length ? -1 : 1;
  }
  for (let index = 0; index < Math.min(a.prerelease.length, b.prerelease.length); index += 1) {
    const x = a.prerelease[index];
    const y = b.prerelease[index];
    if (x === y) continue;
    const xNumeric = /^\d+$/.test(x);
    const yNumeric = /^\d+$/.test(y);
    if (xNumeric && yNumeric) {
      // Digit strings without leading zeros: longer is larger, at any length.
      return x.length < y.length || (x.length === y.length && x < y) ? -1 : 1;
    }
    if (xNumeric) return -1;
    if (yNumeric) return 1;
    return x < y ? -1 : 1;
  }
  return Math.sign(a.prerelease.length - b.prerelease.length);
}

export function isNewerVersion(candidate: string, current: string): boolean {
  return compareVersions(candidate, current) === 1;
}

/**
 * The oldest Mewla on a computer that this app works with. A release that
 * needs the app and the computer to move together raises this to its own
 * version; Settings then asks the person to update the computer. It is never
 * newer than the release it ships in (scripts/prepare-release.py enforces it).
 */
export const MIN_COMPUTER_VERSION = "0.2.88";

/**
 * The version Settings holds the computer to. An app never needs a computer
 * newer than itself: a build of the previous release, or a development build,
 * paired with a computer of its own version is a coherent pair.
 */
export function effectiveMinComputerVersion(min: string, appVersion: string): string {
  return compareVersions(appVersion, min) === -1 ? appVersion : min;
}
