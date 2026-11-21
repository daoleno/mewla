import { context, Text, View, untracked } from "nsh";
import type { TrayRow, TrayTile } from "../../core/tray/model";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon } from "../kit/Icon";
import { ProgressHalo } from "../kit/ProgressHalo";
import type { ProgressMark } from "../../core/work/judgedProgress";

/**
 * The tray in Mewla for Mac's left pane (RESULT-DESIGN-PAIRS #6): rows 40 tall on the material, no card, title 13/17
 * and line 11.5/15. TrayBody provides it for its pane on the Mac; everywhere else the phone's rows.
 */
export const MacPane = context(false);

/**
 * The parts of a tray row (SPEC §11 Tray row), shared by the tray, its left pane and All Sessions.
 * Each reads its row through an accessor inside JSX: a component runs once, and a List cell is recycled.
 */

/**
 * The 28 pt tile: the agent's logo; Brain's mark in ink for a Work with several Workers or none; or an icon. A Work
 * with a judged progress (§11 Progress) has its ring around the tile, 32 pt over the same slot, and a 20 pt logo.
 */
export function RowTile(props: { tile: () => TrayTile; mark?: () => ProgressMark | undefined }) {
  const ringed = () => Boolean(props.mark?.());
  const agentLogo = () => { const tile = props.tile(); return tile.kind === "agent" ? { ...tile, ringed: ringed() } : undefined; };
  // The Mac's pane (win.css `.lp .rows .trow .agent`): a 16 pt mark in a 16 pt slot, its ring 22 around it.
  const mac = MacPane();
  const t = mac ? { slot: 16, halo: 22, icon: 16, brain: 16, brainRinged: 11, logo: 16, logoRinged: 11, framed: 13, framedRinged: 9 } : { slot: 28, halo: 32, icon: 24, brain: 26, brainRinged: 18, logo: 26, logoRinged: 20, framed: 18, framedRinged: 14 };
  return (
    <View class="items-center justify-center" style={{ width: t.slot, height: t.slot }}>
      <If when={props.mark?.()}>{(mark) => <View class="absolute" style={{ left: (t.slot - t.halo) / 2, top: (t.slot - t.halo) / 2 }}><ProgressHalo mark={() => mark()!} size={t.halo} /></View>}</If>
      <If when={props.tile().kind === "agent"} else={<If when={props.tile().kind === "brain"} else={<If when={props.tile().kind === "device"} else={<Icon name="calendar" size={t.icon} tone="soft" />}><Icon name="shield-check" size={t.icon} tone="soft" /></If>}><Icon name="brain" size={ringed() ? t.brainRinged : t.brain} tone="ink" /></If>}>
        {/* Kept by agent, flavor and ring: the row's tile is a new object each time the tray is made again (every second). */}
        <Each of={agentLogo() ? [agentLogo()!] : []} key={(logo) => `${logo.agent}|${logo.flavor}|${logo.ringed}`}>{(logo) => {
          // Read once: the key is agent, flavor and ring, so they cannot change here, and a read inside JSX would make
          // the logo again each time the row object is new.
          const { agent, flavor, ringed: inRing } = untracked(logo);
          // Grok and a plain terminal draw their own frame around the mark (AgentIcon), so their mark is smaller.
          const framed = agent === "terminal" || agent === "grok";
          return <AgentIcon kind={agent} flavor={flavor} size={inRing ? (framed ? t.framedRinged : t.logoRinged) : framed ? t.framed : t.logo} slot={t.slot} />;
        }}</Each>
      </If>
    </View>
  );
}

/** The title 17/600 (400 when done) and the line 15/20 soft, each one line with an ellipsis. */
export function RowText(props: { row: () => TrayRow }) {
  const row = props.row;
  const mac = MacPane();
  return (
    <View class="min-w-0 flex-1">
      <Text testID="row-title" class={`${mac ? "text-[13px] leading-[17px]" : "text-body"} line-clamp-1 ${row().done ? "" : "font-semibold"}`}>{row().title}</Text>
      <If when={row().line}><Text testID="row-line" class={`${mac ? "text-[11.5px] leading-[15px]" : "text-sub"} text-muted-foreground line-clamp-1`}>{row().line}</Text></If>
    </View>
  );
}

function Dot(props: { tone: "ink" | "seal"; testID: string }) {
  // 8 pt; 6 in the Mac's pane (mac.css `.trow .age::before`).
  return <View testID={props.testID} class={`${MacPane() ? "size-1.5" : "size-2"} rounded-full ${props.tone === "seal" ? "bg-seal" : "bg-foreground"}`} />;
}

/**
 * The end, 15 soft: "● 12m" while it runs (a live 8 pt ink dot and how long); a red dot when it waits for
 * you; an unread result's ink dot, then its time and a faint check when done; else when it last changed.
 */
export function RowEnd(props: { row: () => TrayRow }) {
  const row = props.row;
  const mac = MacPane();
  return (
    <View testID="row-end" class={`flex-row items-center ${mac ? "gap-1.5" : "gap-2"}`}>
      <If when={row().needs} else={<If when={row().live} else={<If when={row().unread}><Dot tone="ink" testID="tray-unread" /></If>}><Dot tone="ink" testID="row-live" /></If>}><Dot tone="seal" testID="row-needs" /></If>
      <If when={row().end}><Text class={`${mac ? "text-[11.5px]" : "text-sub"} text-muted-foreground`} style={{ fontVariant: ["tabular-nums"] }}>{row().end}</Text></If>
      <If when={row().done}><Icon name="check" size={mac ? 14 : 18} tone="faint" /></If>
    </View>
  );
}

/** The hair under a row, from 58 pt in (from `left` while a check circle leads the row); the last row has none. */
export function RowHair(props: { left?: () => number }) {
  return <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: props.left?.() ?? 58 }} />;
}
