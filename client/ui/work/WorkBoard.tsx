import { computed, Scroll, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { BRAIN_WORK_GROUP_LABELS, BRAIN_WORK_GROUP_ORDER, type BrainWorkGroup, type BrainWorkSlip } from "../../core/work/surface";
import { workRow } from "../../core/tray/model";
import { boardLayout, workRequest } from "../../core/work/board";
import { tray } from "../tray/state";
import { NavPage } from "../shell/NavPage";
import { openWorkDetail } from "../shell/nav";
import { RowTile } from "../tray/rowParts";
import { EmptyState } from "../kit/kit";
import { clock } from "../calendar/clock";
import { workers } from "../session";
import { workSurface } from "./state";

/** What the tray's Needs you asks of a Work (its own question, or one of its Sessions'): the board files it there too. */
const asked = (slip: BrainWorkSlip) => workRequest(slip, tray().needs);
const groupOf = (slip: BrainWorkSlip): BrainWorkGroup => (slip.group !== "needs" && asked(slip) ? "needs" : slip.group);

/** The board's groups that hold Work, in the surface's order (Needs you first). */
const groups = computed(() => {
  const slips = workSurface().slips.filter((slip) => !slip.mergedInto);
  return BRAIN_WORK_GROUP_ORDER.map((group) => ({ group, slips: slips.filter((slip) => groupOf(slip) === group) })).filter((g) => g.slips.length > 0);
});

/**
 * The Work board (client/DESIGN.md "The wide layout", Beside): what the beside pane shows while nothing is open beside
 * Brain. Brain's Work by what it asks of you, one card each; a card opens its Work detail in the pane. Its groups stand
 * side by side over the whole pane, the longest taking the spare card tracks as a grid (core/work/board.ts), so the
 * pane is filled at any width and any number of Works.
 */
export function WorkBoard() {
  const width = signal(0);
  const sessions = computed(() => new Map(workers().map((w) => [w.id, w] as const)));
  const layout = computed(() => boardLayout(width(), groups().map((g) => g.slips.length)));
  return (
    <NavPage title="Work" testID="work-board" column="full" frame={false}>
      <View class="flex-1 bg-background" onLayout={({ bounds }) => width.set(bounds.size.width)}>
        <If when={groups().length > 0} else={<EmptyState icon="brain" title="No Work" testID="work-board-empty" />}>
          <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="px-6 pt-2 pb-10">
            <View class="flex-row items-start gap-4">
              <Each of={layout().columns} key={(column) => `${column.groups.join(",")}:${column.width}`}>{(column) => (
                <View testID="work-board-column" class="gap-2" style={{ width: column().width }}>
                  <Each of={column().groups} key={(index) => groups()[index]!.group}>{(index) => (
                    <View testID={`work-board-${groups()[index()]!.group}`} class="gap-3 pb-3">
                      <GroupLabel group={groups()[index()]!.group} count={groups()[index()]!.slips.length} />
                      <View class="flex-row flex-wrap gap-4">
                        <Each of={groups()[index()]!.slips} key={(slip) => slip.workId}>{(slip) => (
                          <View style={{ width: layout().cardWidth }}>
                            <BoardCard slip={slip} sessions={sessions} />
                          </View>
                        )}</Each>
                      </View>
                    </View>
                  )}</Each>
                </View>
              )}</Each>
            </View>
          </Scroll>
        </If>
      </View>
    </NavPage>
  );
}

function GroupLabel(props: { group: BrainWorkGroup; count: number }) {
  return (
    <View class="mt-3 flex-row items-center gap-2 px-1">
      <If when={props.group === "needs"}><View class="size-2 rounded-full bg-seal" /></If>
      <Text class="text-sub font-semibold text-muted-foreground">{BRAIN_WORK_GROUP_LABELS[props.group]}</Text>
      <Text class="text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{String(props.count)}</Text>
    </View>
  );
}

/** One Work: its Worker's logo (Brain's mark for several), the title, where it stands, and what it asks of you. */
function BoardCard(props: { slip: () => BrainWorkSlip; sessions: () => ReadonlyMap<string, ReturnType<typeof workers>[number]> }) {
  const row = computed(() => workRow(props.slip(), props.sessions(), clock().now));
  const needs = () => groupOf(props.slip()) === "needs";
  const asks = () => (needs() ? props.slip().question ?? asked(props.slip())?.title ?? "" : "");
  return (
    <Button testID="work-board-card" accessibilityLabel={[row().title, row().line, row().end].filter(Boolean).join(", ")} onPress={() => openWorkDetail(props.slip().workId)}
      class="gap-2 rounded-(--radius-group) bg-sheet-surface px-4 py-3.5 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
      <View class="flex-row items-center gap-3">
        <RowTile tile={() => row().tile} mark={() => row().mark} />
        <Text class={`min-w-0 flex-1 text-body line-clamp-1 ${row().done ? "" : "font-semibold"}`}>{row().title}</Text>
        <View class="flex-row items-center gap-2">
          <If when={needs()} else={<If when={row().live}><View class="size-2 rounded-full bg-foreground" /></If>}><View class="size-2 rounded-full bg-seal" /></If>
          <If when={row().end}><Text class="text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{row().end}</Text></If>
        </View>
      </View>
      <If when={asks()}><Text testID="work-board-asks" class="text-body font-semibold line-clamp-3">{asks()}</Text></If>
      <If when={row().line && row().line !== asks() && row().line !== row().end}><Text testID="work-board-line" class="text-sub text-muted-foreground line-clamp-3">{row().line}</Text></If>
    </Button>
  );
}
