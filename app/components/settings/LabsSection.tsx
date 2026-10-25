import React from "react";
import { ListRow, ListSection } from "../ui";
import { InkSwitch } from "../plugins/PluginConnectionViews";
import { useCatLabs } from "../../store/catLabs";

/** Things still being tried. Today one: the cat's tab, kept on the computer. */
export function LabsSection() {
  const { enabled, available, setEnabled } = useCatLabs();
  return (
    <ListSection>
      <ListRow
        title="Cat"
        subtitle={available ? "Brain's cat. It travels while your Workers run" : "Connect a computer first"}
        icon="paw"
        disabled={!available}
        accessibilityRole="switch"
        trailing={
          <InkSwitch
            value={enabled}
            disabled={!available}
            accessibilityLabel="Cat"
            onValueChange={setEnabled}
          />
        }
      />
    </ListSection>
  );
}
