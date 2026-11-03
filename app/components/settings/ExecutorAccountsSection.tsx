import React, { useMemo } from "react";
import { ListRow, ListSection } from "../ui/ListSection";
import { useBrain } from "../../store/brain";
import { executorAccountRows } from "./executorAccountRows";

export function ExecutorAccountsSection({ serverId }: { serverId: string | null }) {
  const { state } = useBrain();
  const brain = serverId ? state.byServer[serverId] : undefined;
  const rows = useMemo(() => executorAccountRows(brain?.executors ?? [], brain?.executor_states), [brain?.executors, brain?.executor_states]);
  if (rows.length === 0) return null;
  return (
    <ListSection>
      {rows.map((row) => (
        <ListRow key={row.id} title={row.name} value={row.value} icon="person" accessibilityLabel={`${row.name}, ${row.value}`} />
      ))}
    </ListSection>
  );
}
