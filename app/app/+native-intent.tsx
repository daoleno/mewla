import { isPluginReturnUrl } from "../services/pluginOnboarding";

/**
 * A running app receives plugin authorization returns (mewla://plugins?state=… or legacy zen://plugins?state=…)
 * through the Plugins flow's own Linking listener. Routing them as well would
 * pop the Plugins stack to its catalog mid-connection, so they are dropped
 * here. A cold start still opens Plugins, which restores and finishes the flow.
 */
export function redirectSystemPath({ path, initial }: { path: string; initial: boolean }) {
  return !initial && isPluginReturnUrl(path) ? null : path;
}
