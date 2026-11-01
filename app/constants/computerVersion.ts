import { compareVersions } from "../services/appUpdate";

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
