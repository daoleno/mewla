import { computed, keyboardFrame, safeArea, Text, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { RAIL, RAIL_PLACES, railEnds, railLayout, railPageOrder, railRoom, type RailPlaceId } from "../../core/shell/rail";
import { screenRoute } from "../../core/screen/float";
import { backMark } from "../../core/tray/model";
import { Icon, type IconName } from "../kit/Icon";
import { showMenu } from "../kit/Menu";
import { newSessionOpen } from "../screens/NewSession";
import { StateDotMark } from "../tray/StateControl";
import { brainTurn, refreshOpened, tray } from "../tray/state";
import { paletteOpen } from "./desktop/Overlays";
import { rail, wide } from "./layout";
import { railPages, type RailPressable as Pressable } from "./railPages";
import { goHome, homeShown, nav, openPage, trayOpen } from "./nav";

/** The shown page's controls in the rail, Back first when there is a page to go back to. */
const pageControls = computed((): Pressable[] => {
  const own = (railPages().at(-1)?.actions() ?? []).filter((action) => action.shown?.() ?? true);
  const back: Pressable[] = nav.routes().length > 1 ? [{ id: "back", label: backMark(tray().state.kind)?.label ?? "Back", icon: "chevron-left" as const, onPress: () => nav.pop() }] : [];
  return railPageOrder<Pressable & { transient?: boolean }>(back, own);
});

/** State: the tray, over Brain (home's own overlay); Pair again goes straight to pairing. */
function openState() {
  if (untracked(tray).state.kind === "pair") return openPage({ name: "computer" }, { name: "pair" });
  if (!untracked(homeShown)) return goHome({ tray: true });
  if (!untracked(trayOpen)) refreshOpened();
  trayOpen.set(!untracked(trayOpen));
}

const PLACE_PRESS: Record<RailPlaceId, () => void> = {
  brain: () => goHome(),
  state: openState,
  search: () => paletteOpen.set(true),
  screen: () => openPage({ name: "computer" }, screenRoute(null)),
  new: () => newSessionOpen.set(true),
};
const PLACE_ICON: Record<RailPlaceId, IconName> = { brain: "brain", state: "pulse", search: "search", screen: "desktop", new: "add" };

/** The fixed places shown: State leaves while the left pane holds the tray, and with no computer. */
const places = computed((): Pressable[] =>
  RAIL_PLACES.filter((place) => place.id !== "state" || (!wide() && tray().state.kind !== "none")).map((place) => ({
    id: place.id,
    label: place.id === "state" ? tray().state.words : place.label,
    icon: PLACE_ICON[place.id],
    onPress: PLACE_PRESS[place.id],
  })),
);

/** The bar's part that is the rail's: past the system's items, and above the keyboard while it is up. */
const ends = computed(() => railEnds(windowSize(), safeArea(), rail(), keyboardFrame().height));
const layout = computed(() => railLayout(pageControls(), places(), railRoom(windowSize().height, ends())));

/** A rail button: 44 pt, the glyph 22, the accent well while it is where the person is. Made once per id (`Each`): the
 * item's label, action and state are read through `item`, so a change elsewhere never remakes the button under a finger. */
function RailButton(props: { item: () => Pressable; selected?: () => boolean; children: JSX.Element }) {
  return (
    <Button ref={props.item().ref} testID={`rail-${props.item().id}`} accessibilityLabel={props.item().label} onPress={() => props.item().onPress()} disabled={props.item().disabled?.()}
      class={`items-center justify-center rounded-full active:bg-accent disabled:opacity-40 ${props.selected?.() ? "bg-accent" : ""}`} style={{ width: RAIL.target, height: RAIL.target }}>
      {props.children}
    </Button>
  );
}

/** The 8 pt dot at a button's top-right: Back's connection mark, Brain's turn running. */
function CornerDot(props: { tone: "grey" | "red" | "ink"; testID: string }) {
  const tone = () => (props.tone === "red" ? "bg-seal" : props.tone === "ink" ? "bg-foreground" : "bg-muted-foreground");
  return <View testID={props.testID} class={`absolute right-1.5 top-1.5 size-2 rounded-full ${tone()}`} />;
}

function Item(props: { item: () => Pressable }) {
  const id = untracked(props.item).id;
  if (id === "back") {
    const mark = () => backMark(tray().state.kind);
    return (
      <RailButton item={props.item}>
        <Icon name="chevron-left" size={RAIL.glyph} tone="ink" />
        <If when={mark()}>{(m) => <CornerDot testID="rail-back-mark" tone={m().tone} />}</If>
      </RailButton>
    );
  }
  if (id === "brain") {
    return (
      <RailButton item={props.item} selected={homeShown}>
        <Icon name="brain" size={RAIL.glyph} tone="ink" />
        <If when={brainTurn()}><CornerDot testID="rail-brain-live" tone="ink" /></If>
      </RailButton>
    );
  }
  if (id === "state") {
    // The state's dot, and its count while something needs the person.
    const count = () => (tray().state.kind === "needs" ? tray().state.count ?? 0 : 0);
    return (
      <RailButton item={props.item} selected={() => homeShown() && trayOpen()}>
        <View class="flex-row items-center gap-1">
          <StateDotMark dot={tray().state.dot} />
          <If when={count() > 0}><Text testID="rail-state-count" class="text-sub font-semibold text-foreground">{String(count())}</Text></If>
          <If when={count() === 0 && !tray().state.dot}><Icon name="pulse" size={RAIL.glyph} tone="ink" /></If>
        </View>
      </RailButton>
    );
  }
  // The glyph is made once with its button; only its name follows the item.
  return <RailButton item={props.item}><Icon name={props.item().icon ?? "dot"} size={RAIL.glyph} tone="ink" /></RailButton>;
}

/** The rail's buttons in order, each made once per id and kept while its id stays. */
function Items(props: { of: () => readonly Pressable[] }) {
  return <Each of={props.of()} key={(item: Pressable) => item.id}>{(item: () => Pressable) => <Item item={item} />}</Each>;
}

/** What did not fit: one More at the foot, its menu in the rail's order. */
function More(props: { items: readonly Pressable[] }) {
  const press = async () => {
    const items = untracked(() => props.items);
    const chosen = await showMenu({ items: items.map((item) => ({ id: item.id, label: item.label, icon: item.icon, disabled: item.disabled?.() })), testID: "rail-more-menu" });
    items.find((item) => item.id === chosen)?.onPress();
  };
  const more: Pressable = { id: "more", label: "More", icon: "more-horizontal", onPress: () => void press() };
  return <RailButton item={() => more}><Icon name="more-horizontal" size={RAIL.glyph} tone="ink" /></RailButton>;
}

/**
 * The side rail (client/DESIGN.md "The side rail"), in the system's vertical bar whenever there is one (`rail`):
 * the page's controls from under the system's items down, the places at the foot. The bar is the system's: the
 * rail paints nothing of its own, and a press anywhere but on a button does nothing.
 */
export function Rail() {
  const width = () => (rail() === "left" ? safeArea().left : safeArea().right);
  return (
    <><If when={rail()}><View testID="rail" class="absolute bottom-0 top-0 items-center" role="toolbar"
        style={{ width: width(), left: rail() === "left" ? 0 : undefined, right: rail() === "right" ? 0 : undefined, paddingTop: ends().top, paddingBottom: ends().bottom, pointerEvents: "box-none" }}>
        <View testID="rail-page" class="items-center"><Items of={() => layout().top} /></View>
        <View class="flex-1" style={{ minHeight: RAIL.groupGap, pointerEvents: "none" }} />
        <View testID="rail-places" class="items-center">
          <Items of={() => layout().bottom} />
          <If when={layout().more.length > 0}><More items={layout().more} /></If>
        </View>
      </View></If></>
  );
}
