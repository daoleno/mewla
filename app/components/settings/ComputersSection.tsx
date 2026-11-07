import React from "react";
import type { PathMark } from "../../services/connectionPath";
import { ConnectionPathMark } from "../navigation/ConnectionPathMark";
import { ListRow, ListSection } from "../ui/ListSection";

export interface ComputerRow {
  id: string;
  name: string;
  /** One word, such as Online, Offline or In use; empty until known. */
  status: string;
  /** How this phone reaches it; set only on the computer in use. */
  mark?: PathMark | null;
}

/** The computers on this phone: a name and one status word each. */
export function ComputersSection({
  computers,
  onOpen,
  onPair,
  onShowPath,
}: {
  computers: ComputerRow[];
  onOpen: (id: string) => void;
  onPair: () => void;
  /** A long press on the row reveals the detail. */
  onShowPath?: (mark: PathMark) => void;
}) {
  return (
    <ListSection>
      {computers.map((computer) => (
        <ListRow
          key={computer.id}
          title={computer.name}
          value={computer.status}
          icon="desktop"
          accessory="chevron"
          trailing={computer.mark ? <ConnectionPathMark mark={computer.mark} /> : undefined}
          accessibilityLabel={[computer.name, computer.status, computer.mark?.label].filter(Boolean).join(", ")}
          onPress={() => onOpen(computer.id)}
          onLongPress={computer.mark && onShowPath ? () => onShowPath(computer.mark!) : undefined}
        />
      ))}
      <ListRow title="Pair a computer" icon="add" accessibilityLabel="Pair a computer" accessory="chevron" onPress={onPair} />
    </ListSection>
  );
}
