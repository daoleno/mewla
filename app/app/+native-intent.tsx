import { redirectLivePath } from "../services/liveSurface";
import { isFriendsReturnUrl } from "../services/friendsContract";
import { isPluginReturnUrl } from "../services/pluginOnboarding";

/**
 * A running app receives plugin authorization returns (mewla://plugins?state=…)
 * through the Plugins flow's own Linking listener. Routing them as well would
 * pop the Plugins stack to its catalog mid-connection, so they are dropped
 * here. A cold start still opens Plugins, which restores and finishes the flow.
 *
 * Friends sign-in returns (mewla://friends/enroll?…) are read by the browser
 * session that started them; routing them would open a screen that is not there.
 *
 * Live notification, chip and widget links (mewla://live/…) open Brain.
 */
export function redirectSystemPath({ path, initial }: { path: string; initial: boolean }) {
  if (!initial && isPluginReturnUrl(path)) return null;
  if (isFriendsReturnUrl(path) || path.startsWith("/friends/enroll")) return null;
  return redirectLivePath(path);
}
