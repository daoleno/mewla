import { effect, lifecycle, Text } from "nsh";
import { Button } from "nsh/base";
import { Row } from "../kit/kit";
import { tile } from "../kit/RowTile";
import { checkOldApp, oldAppInstalled, removeOldApp } from "./moveIn";

/**
 * Settings → About: while the Mewla this one replaced is still installed, the one thing to do about it. It looks
 * again whenever the app comes back to the front, so the row goes once the system's uninstall dialog was answered.
 */
export function OldAppRow() {
  effect(() => {
    if (lifecycle() === "active") void checkOldApp();
  });
  return (
    <If when={oldAppInstalled()}>
      <Row
        testID="old-app"
        lead={tile("trash")}
        title="Old Mewla is still installed"
        trailing={
          <Button testID="old-app-remove" onPress={() => void removeOldApp()} class="rounded-full bg-primary px-3.5 py-1.5 active:opacity-70">
            <Text class="text-sm font-medium text-primary-foreground">Remove</Text>
          </Button>
        }
      />
    </If>
  );
}
