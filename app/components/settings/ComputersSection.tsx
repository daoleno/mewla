import React from "react";
import { ListRow, ListSection } from "../ui/ListSection";

export interface ComputerRow {
  id: string;
  name: string;
  /** One word, such as Online, Offline or In use; empty until known. */
  status: string;
}

/** The computers on this phone: a name and one status word each. */
export function ComputersSection({
  computers,
  onOpen,
  onPair,
}: {
  computers: ComputerRow[];
  onOpen: (id: string) => void;
  onPair: () => void;
}) {
  return (
    <ListSection
      footer={computers.length === 0 ? "Pair this phone with a computer running Mewla. On the computer, run mewla pair." : null}
    >
      {computers.map((computer) => (
        <ListRow
          key={computer.id}
          title={computer.name}
          value={computer.status}
          icon="desktop"
          accessory="chevron"
          accessibilityLabel={computer.status ? `${computer.name}, ${computer.status}` : computer.name}
          onPress={() => onOpen(computer.id)}
        />
      ))}
      <ListRow title="Pair a computer" icon="add" accessibilityLabel="Pair a computer" accessory="chevron" onPress={onPair} />
    </ListSection>
  );
}
