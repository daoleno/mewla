import { type Accessor, Text, View } from "nsh";
import { Button } from "nsh/base";
import { appUpdateRow, type AppUpdateRowModel } from "../../core/update/appUpdate";
import { Meter, Row, Spinner } from "../kit/kit";
import { tile } from "../kit/RowTile";
import { appUpdateOwner, type AppUpdate } from "./update";

/**
 * Settings → About, under the version: where this build's update stands in a few
 * words and its one action (Check for updates, Up to date, Retry, Update available
 * vX [Update], Downloading NN%, Allow app installs [Continue], Ready to install vX
 * [Install], Update failed [Retry]). Nothing where the build cannot update itself.
 */
export function AboutUpdateRow(props: { update?: AppUpdate }) {
  const update = () => props.update ?? appUpdateOwner();
  const model = () => appUpdateRow(update().state());
  // Made again when what it says or does changes (a Row takes its tap once); a download's ticks only move its meter.
  return (
    <View>
      <Each of={model() ? [model()!] : []} key={rowKey}>{(row) => <UpdateRow model={row} press={() => update().press()} />}</Each>
    </View>
  );
}

function rowKey(model: AppUpdateRowModel): string {
  return model.progress !== undefined ? "downloading" : JSON.stringify(model);
}

function UpdateRow(props: { model: Accessor<AppUpdateRowModel>; press: () => void }) {
  const model = props.model;
  return (
    <Row
      testID="app-update"
      lead={tile(model().progress !== undefined ? "download" : "refresh")}
      title={model().title}
      subtitle={model().subtitle}
      value={model().value}
      // The button is the action; without one, a tap checks again ("Up to date").
      onPress={model().actionable && !model().action ? props.press : undefined}
      trailing={<If when={model().action} else={<If when={model().progress !== undefined} else={<If when={model().busy}><Spinner size={16} /></If>}><View style={{ width: 64 }}>
            <Meter share={model().progress!} tone="meter" height={4} />
          </View></If>}>{(actionValue) => <Button testID="app-update-action" onPress={props.press} class="rounded-full bg-primary px-3.5 py-1.5 active:opacity-70">
            <Text class="text-sm font-medium text-primary-foreground">{actionValue()}</Text>
          </Button>}</If>}
    />
  );
}
