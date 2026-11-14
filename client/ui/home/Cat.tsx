import { computed, effect, keyboardFrame, signal, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { brainSurface } from "../../core/brain";
import { catState, deliveredSince } from "../../core/pets/cat";
import { workOfSnapshot } from "../../core/work/brainWork";
import { PetSprite } from "../pets/PetSprite";
import { scrolling } from "../pets/hold";
import { brain, brainThread, connection, current } from "../session";
import { homeShown } from "../shell/nav";
import { tray } from "../tray/state";
import { catNeedsYou } from "../computer/ComputerPage";
import { catUpdateHint } from "../update/hint";

/** The cat is the computer (SPEC §1.3, §5), from the same model as the state control. */
const state = computed(() => {
  const t = tray();
  return catState({
    kind: t.state.kind,
    starting: brainSurface({ hasServer: Boolean(current()), connected: connection() === "connected", snapshot: brain() }) === "waking",
    needs: t.needs.length,
    brainTurn: brainThread().conversation?.activity?.status === "running",
    // The count the state control says: quiet rows (queued and waiting Work) under Running don't wake it.
    running: t.runningCount,
  });
});

/** Each Work that came back done since the last look plays `delivered` once; a computer switch only takes stock. */
const delivered = signal(0);
let unreadSeen: Set<string> | null = null;
let seenServer: string | null = null;
effect(() => {
  const snapshot = brain() as Record<string, unknown> | null;
  const server = current()?.id ?? null;
  const work = workOfSnapshot(snapshot);
  const unread = new Set(work.current.filter((w) => w.unread_result).map((w) => w.work_id));
  untracked(() => {
    if (server !== seenServer || !snapshot) unreadSeen = null;
    seenServer = server;
    if (deliveredSince(unreadSeen, unread)) delivered.set(delivered() + 1);
    if (snapshot) unreadSeen = unread;
  });
});

/**
 * The header's cat: 40 pt in a 48 pt round target that opens the computer page, a red dot when
 * something behind it needs you, else a smaller ink dot while an update not yet looked at waits. A change of state plays its clip once, then the still holds; it
 * holds still while the person scrolls or types and while a page is pushed over home. The bleed
 * clips rise above the 40 pt box: nothing here clips them, and the header keeps its height.
 */
export function HomeCat(props: { onPress?: () => void; /** The still's size (40) and its round target's (48); the capsule's are 32 and 44. */ size?: number; box?: number }) {
  const canMove = () => homeShown() && !scrolling() && keyboardFrame().height === 0;
  const box = props.box ?? 48;
  const mark = () => (
    <>
      <PetSprite testID="home-cat-sprite" state={state} size={props.size ?? 40} mode="once" canMove={canMove} delivered={delivered} />
      <If when={catNeedsYou()} else={<If when={catUpdateHint()}><View testID="home-cat-update" accessibilityLabel="Update available" class="absolute right-1 top-1 size-3 rounded-full border-2 border-background bg-primary" /></If>}><View testID="home-cat-dot" accessibilityLabel="Needs you" class="absolute right-1 top-1 size-[14px] rounded-full border-2 border-background bg-seal" /></If>
    </>
  );
  // Without onPress it is part of a larger target (the home header's left block), not a button of its own.
  return <If when={props.onPress} else={<View testID="home-cat-mark" class="items-center justify-center" style={{ overflow: "visible", width: box, height: box }}>{mark()}</View>}>{(onPressValue) => <Button testID="home-cat" accessibilityLabel="Computer" onPress={onPressValue()} class="items-center justify-center rounded-full active:bg-accent" style={{ overflow: "visible", width: box, height: box }}>
      {mark()}
    </Button>}</If>;
}
