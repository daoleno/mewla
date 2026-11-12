import { Text, View } from "nsh";
import { Button } from "nsh/base";
import { appUpdateRow, type AppUpdateRowModel } from "../../core/update/appUpdate";
import { Meter, Row, Spinner } from "../kit/kit";
import { appUpdate, type AppUpdate } from "./update";

/**
 * Settings → About, under the version: where this build's update stands in a few
 * words and its one action (Check for updates, Up to date, Retry, Update available
 * vX [Update], Downloading NN%, Allow app installs [Continue], Ready to install vX
 * [Install], Update failed [Retry]). Nothing where the build cannot update itself.
 */
export function AboutUpdateRow(props: { update?: AppUpdate }) {
  const update = () => props.update ?? appUpdate;
  return <View>{appUpdateRow(update().state()) ? updateRow(appUpdateRow(update().state())!, () => update().press()) : null}</View>;
}

function updateRow(model: AppUpdateRowModel, press: () => void) {
  return (
    <Row
      testID="app-update"
      icon={model.progress !== undefined ? "download" : "refresh"}
      title={model.title}
      subtitle={model.subtitle}
      value={model.value}
      // The button is the action; without one, a tap checks again ("Up to date").
      onPress={model.actionable && !model.action ? press : undefined}
      trailing={
        model.action ? (
          <Button testID="app-update-action" onPress={press} class="rounded-full bg-primary px-3.5 py-1.5 active:opacity-70">
            <Text class="text-sm font-medium text-primary-foreground">{model.action}</Text>
          </Button>
        ) : model.progress !== undefined ? (
          <View style={{ width: 64 }}>
            <Meter share={model.progress} tone="ink" height={4} />
          </View>
        ) : model.busy ? (
          <Spinner size={16} />
        ) : undefined
      }
    />
  );
}
