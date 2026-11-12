import { Text, View } from "nsh";
import type { TrayRow, TrayTile } from "../../core/tray/model";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon } from "../kit/Icon";
import { ProgressBar } from "../kit/ProgressBar";

/**
 * The parts of a tray row (SPEC §11 Tray row), shared by the tray, its left pane and All Sessions.
 * Each reads its row through an accessor inside JSX: a component runs once, and a List cell is recycled.
 */

/** The 28 pt tile: the agent's logo; Brain's mark in ink for a Work with several Workers or none; or an icon. */
export function RowTile(props: { tile: () => TrayTile }) {
  return (
    <View class="size-[28px] items-center justify-center">
      {props.tile().kind === "agent" ? (() => {
        const tile = props.tile() as Extract<TrayTile, { kind: "agent" }>;
        // Grok and a plain terminal draw their own frame around the mark (AgentIcon), so their mark is smaller.
        const framed = tile.agent === "terminal" || tile.agent === "grok";
        return <AgentIcon kind={tile.agent} flavor={tile.flavor} size={framed ? 18 : 26} slot={28} />;
      })()
        : props.tile().kind === "brain" ? <Icon name="brain" size={26} tone="ink" />
        : props.tile().kind === "device" ? <Icon name="shield-check" size={24} tone="soft" />
        : <Icon name="calendar" size={24} tone="soft" />}
    </View>
  );
}

/** The title 17/600 (400 when done) and the line 15/20 soft, each one line with an ellipsis; then the progress bar. */
export function RowText(props: { row: () => TrayRow }) {
  const row = props.row;
  return (
    <View class="min-w-0 flex-1">
      <Text testID="row-title" class={`text-body line-clamp-1 ${row().done ? "" : "font-semibold"}`}>{row().title}</Text>
      {row().line ? <Text testID="row-line" class="text-sub text-muted-foreground line-clamp-1">{row().line}</Text> : null}
      {row().progress !== undefined ? <ProgressBar filled={row().progress!} testID="tray-progress" /> : null}
    </View>
  );
}

function Dot(props: { tone: "ink" | "seal"; testID: string }) {
  return <View testID={props.testID} class={`size-2 rounded-full ${props.tone === "seal" ? "bg-seal" : "bg-foreground"}`} />;
}

/**
 * The end, 15 soft: "● 12m" while it runs (a live 8 pt ink dot and how long); a red dot when it waits for
 * you; an unread result's ink dot, then its time and a faint check when done; else when it last changed.
 */
export function RowEnd(props: { row: () => TrayRow }) {
  const row = props.row;
  return (
    <View testID="row-end" class="flex-row items-center gap-2">
      {row().needs ? <Dot tone="seal" testID="row-needs" />
        : row().live ? <Dot tone="ink" testID="row-live" />
        : row().unread ? <Dot tone="ink" testID="tray-unread" />
        : null}
      {row().end ? <Text class="text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{row().end}</Text> : null}
      {row().done ? <Icon name="check" size={18} tone="faint" /> : null}
    </View>
  );
}

/** The hair under a row, from 58 pt in (from `left` while a check circle leads the row); the last row has none. */
export function RowHair(props: { left?: () => number }) {
  return <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: props.left?.() ?? 58 }} />;
}
