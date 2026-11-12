import { cleanup, effect, Overlay, signal, Text, untracked, View, type ViewRef } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { wide } from "../shell/layout";
import { Icon, type IconName } from "./Icon";
import { showAlert } from "../shell/alert";

/** One action of an action menu (Brain's ⋯, a Session's ⋯, the tray's ⋯, Git ⋯, Screen ⋯). */
export interface MenuItem {
  id: string;
  label: string;
  icon?: IconName;
  /** A value at the row's end ("Opus · High"). */
  value?: string;
  /** Something drawn at the row's end (an agent's logo). */
  trailing?: JSX.Element;
  /** Stays in place at 40 % and does nothing (SPEC §1.6). A phone's action sheet leaves it out. */
  disabled?: boolean;
  destructive?: boolean;
  /** One choice of a set, checked. */
  checked?: boolean;
}

export interface MenuRequest {
  title?: string;
  items: readonly MenuItem[];
  /** The button that opened it: on wide the popover sits at it. */
  anchor?: ViewRef | null;
  testID?: string;
}

/** What a button hands an action menu to sit at: `ref` on the button, `node()` for the request. */
export function createMenuAnchor(): { ref: (node: ViewRef) => void; node: () => ViewRef | null } {
  let node: ViewRef | null = null;
  return { ref: (next) => void (node = next), node: () => node };
}

const shown = signal<{ request: MenuRequest; resolve: (id: string | null) => void } | null>(null);
/** A host is mounted to show popovers (the shell's); without one every menu is the action sheet. */
let hosts = 0;

/**
 * An action menu: on a phone and a narrow window the platform's action sheet; on wide (soft-final
 * SPEC §14.2) a popover at its button, 288 wide, rows 48, radius 18, which Esc and a press outside
 * close. Resolves with the chosen item's id, or null when it closed without one.
 */
export function showMenu(request: MenuRequest): Promise<string | null> {
  if (untracked(wide) && request.anchor && hosts > 0) {
    untracked(shown)?.resolve(null);
    return new Promise((resolve) => shown.set({ request, resolve }));
  }
  const actions = request.items
    .filter((item) => !item.disabled)
    .map((item) => ({ id: item.id, label: item.checked ? `✓ ${item.label}` : item.label, role: item.destructive ? ("destructive" as const) : undefined }));
  return showAlert({ title: request.title, presentation: "actionSheet", actions: [...actions, { id: "cancel", label: "Cancel", role: "cancel" }] })
    .then((answer) => (answer.action && answer.action !== "cancel" ? answer.action : null))
    .catch(() => null);
}

/** One row of a popover menu: 48 pt, an optional glyph, the label, its value or trailing; the hover well. */
function MenuRow(props: { item: MenuItem; onPress: () => void }) {
  const item = () => props.item;
  return (
    <Button testID={`menu-item-${item().id}`} accessibilityLabel={item().label} role="menuitem" disabled={item().disabled} onPress={props.onPress}
      class="mx-1.5 min-h-[48px] flex-row items-center gap-3 rounded-[12px] px-3 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-40">
      {item().icon ? <Icon name={item().icon!} size={20} tone="ink" /> : null}
      <Text class="min-w-0 flex-1 text-body line-clamp-1">{item().label}</Text>
      {item().value ? <Text class="text-sub text-muted-foreground line-clamp-1">{item().value}</Text> : null}
      {item().trailing ?? null}
      {item().checked ? <Icon name="check" size={18} tone="ink" /> : null}
    </Button>
  );
}

/**
 * A popover menu at `anchor` (SPEC §14.2): a card 288 wide with 48 pt rows and radius 18, below its
 * button (above when there is no room below), right-aligned to it. Esc and a press outside close it.
 */
export function MenuPopover(props: {
  open: () => boolean;
  anchor: () => ViewRef | null;
  items: () => readonly MenuItem[];
  onSelect: (id: string) => void;
  onClose: () => void;
  title?: () => string | undefined;
  testID?: string;
}) {
  return (
    <>{props.open() && props.anchor() ? (
      <Overlay anchor={props.anchor()} placement="bottom" align="end" offset={6} margin={12} onPressOutside={props.onClose} onCancel={props.onClose}>
        <View testID={props.testID ?? "menu-popover"} role="menu" accessibilityLabel={props.title?.() ?? "Menu"}
          class="w-[288px] gap-0.5 overflow-hidden rounded-[18px] border border-border bg-popover py-1.5 shadow-float">
          {props.title?.() ? <Text class="px-[18px] pb-1 pt-2 text-caption font-semibold text-muted-foreground line-clamp-1">{props.title!()!}</Text> : null}
          {props.items().map((item) => <MenuRow item={item} onPress={() => props.onSelect(item.id)} />)}
        </View>
      </Overlay>
    ) : null}</>
  );
}

/** Where `showMenu`'s popovers show: mounted once at the root. Narrowing the window closes one. */
export function MenuHost() {
  hosts += 1;
  cleanup(() => void (hosts -= 1));
  const close = (id: string | null) => {
    const open = untracked(shown);
    shown.set(null);
    open?.resolve(id);
  };
  effect(() => {
    if (!wide() && untracked(shown)) close(null);
  });
  return (
    <MenuPopover open={() => Boolean(shown())} anchor={() => shown()?.request.anchor ?? null} items={() => shown()?.request.items ?? []}
      title={() => shown()?.request.title} testID={shown()?.request.testID}
      onSelect={(id) => close(id)} onClose={() => close(null)} />
  );
}
