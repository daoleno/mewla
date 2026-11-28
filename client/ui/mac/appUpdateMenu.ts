import type { AppUpdateState } from "../../core/update/appUpdate";

/**
 * The cat's menu row for this app's update (DESIGN-MAC.md §2.4): its progress while a newer Mewla downloads on its own,
 * "Restart to update to X" once it waits downloaded (the header's Update button, the same press), the failure with a
 * way to try again. "Update to X" only where this install cannot replace itself (the release page opens). Nothing
 * otherwise. A separator comes with it, so the menu
 * has none left over.
 */
export function appUpdateMenuItem(state: AppUpdateState, press: () => void) {
  switch (state.kind) {
    case "available":
      return [{ type: "separator" as const }, { label: `Update to ${state.update.version}`, onSelect: press }];
    case "downloading":
      return [{ type: "separator" as const }, { label: `Updating… ${Math.floor(state.progress * 100)}%`, disabled: true, onSelect: () => {} }];
    case "ready":
      return [{ type: "separator" as const }, { label: `Restart to update to ${state.update.version}`, onSelect: press }];
    case "failed":
      return [{ type: "separator" as const }, { label: `Update to ${state.update.version} failed: try again`, onSelect: press }];
    default:
      return [];
  }
}
