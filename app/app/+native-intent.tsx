import { redirectLivePath } from "../services/liveSurface";
import { redirectPhonePath } from "../services/phoneRoute";
import { isPluginReturnUrl } from "../services/pluginOnboarding";

/**
 * A running app receives plugin authorization returns (mewla://plugins?state=…)
 * through the Plugins flow's own Linking listener. Routing them as well would
 * pop the Plugins stack to its catalog mid-connection, so they are dropped
 * here. A cold start still opens Plugins, which restores and finishes the flow.
 *
 * Live notification, chip and widget links (mewla://live/…) open Brain. Phone use's
 * notifications (mewla://phone) open its page.
 */
export function redirectSystemPath({ path, initial }: { path: string; initial: boolean }) {
  if (!initial && isPluginReturnUrl(path)) return null;
  return redirectPhonePath(redirectLivePath(path));
}
