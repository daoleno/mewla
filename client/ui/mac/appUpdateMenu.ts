import type { AppUpdateState } from "../../core/update/appUpdate";

/**
 * The cat's menu row for this app's update (DESIGN-MAC.md §2.4): "Update to X" while a newer Mewla waits, its progress
 * while it downloads, the failure with a way to try again. Nothing otherwise. A separator comes with it, so the menu
 * has none left over.
 */
export function appUpdateMenuItem(state: AppUpdateState, press: () => void) {
  switch (state.kind) {
    case "available":
      return [{ type: "separator" as const }, { label: `Update to ${state.update.version}`, onSelect: press }];
    case "downloading":
      return [{ type: "separator" as const }, { label: `Updating… ${Math.floor(state.progress * 100)}%`, disabled: true, onSelect: () => {} }];
    case "ready":
      return [{ type: "separator" as const }, { label: "Restarting to update…", disabled: true, onSelect: () => {} }];
    case "failed":
      return [{ type: "separator" as const }, { label: `Update to ${state.update.version} failed: try again`, onSelect: press }];
    default:
      return [];
  }
}
