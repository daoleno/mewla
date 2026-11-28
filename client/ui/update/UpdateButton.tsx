import { Text } from "nsh";
import { Button } from "nsh/base";
import { appUpdateButton, appUpdateOwner } from "./update";

/**
 * Mewla for Mac's Update button, the one a desktop app keeps in its title bar: it shows once the newer app has
 * downloaded on its own, and one click installs it and starts the new app (the update is also staged for the app's
 * quit, so it applies without a click). Nothing before that, and nowhere else but the Mac. The app's own action
 * button (About's), no words around it.
 */
export function UpdateButton() {
  return (
    <If when={appUpdateButton()}>
      <Button testID="update-button" accessibilityLabel="Update" onPress={() => appUpdateOwner().press()}
        class="rounded-full bg-primary px-3.5 py-1.5 hover:opacity-90 active:opacity-70 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
        <Text class="text-sm font-medium text-primary-foreground">Update</Text>
      </Button>
    </If>
  );
}
