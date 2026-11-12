import { cleanup, computed, createMotion, effect, keyboardFrame, PanResponder, reduceMotion, safeArea, signal, Storage, Text, untracked, View, windowSize, type JSX } from "nsh";
import { AlertDialog, Button } from "nsh/base";
import {
  DEFAULT_FLOAT_CORNER,
  FLOAT_CORNER_KEY,
  FLOAT_DEFAULT,
  FLOAT_SIZE_KEY,
  clampFloatFraction,
  floatOrigin,
  floatSize,
  nearestFloatCorner,
  parseFloatCorner,
  parseFloatFraction,
  resizedFloatWidth,
  toggledFloatFraction,
  type FloatCorner,
} from "../../core/screen/float";
import { Icon } from "../kit/Icon";

const EDGE = 12;
/** Under the page header. */
const HEADER = 56;
/** The chat composer, with room for a few lines of draft: the window stays above it. */
const COMPOSER = 140;
const BAR = 30;
/** The resize handle: a touch area around a small grip in the window's inner corner. */
const HANDLE = 32;
/** Two taps on the title this close together are a double tap; a single tap waits this long before it goes to the next screen. */
const DOUBLE_TAP_MS = 300;
/** The window gliding to its corner, as v0.3.1's (220 ms, ease out). */
const SNAP_MS = 220;
/** One breath of the attention ring, in and out as v0.3.1's. */
const BREATH_MS = 700;

/** One entry of the window's menu (an action sheet). */
export interface FloatMenuItem {
  id: string;
  label: string;
  destructive?: boolean;
  run: () => void;
}

export interface ScreenFloatProps {
  /** Width over height of the picture; the window is exactly that shape under its bar. */
  ratio: () => number;
  title: () => string;
  /** How many screens may show here, and which this is; more than one, a tap on the title goes to the next. */
  count: () => number;
  index: () => number;
  onCycle: () => void;
  menu: () => FloatMenuItem[];
  /** Opens this screen's full view. */
  onOpen: () => void;
  /** Keyboard toggle for a screen the person can type on, while there is one. */
  keyboard?: () => { open: boolean; toggle: () => void } | null;
  /** Room under the picture the hosted view needs (its key bar while the system keyboard is up). */
  extraHeight?: () => number;
  /** The person is typing on the screen with the keyboard up: the chat's composer is not in the way. */
  typing?: () => boolean;
  /** Something asks for the person: a ring in the seal colour. */
  attention?: () => boolean;
  /** What lies under the window's area at the bottom (the tab bar). */
  bottomInset?: () => number;
  /** Draws the live view into the picture's exact size. */
  children: (picture: () => { width: number; height: number }) => JSX.Element;
}

const corner = signal<FloatCorner>(DEFAULT_FLOAT_CORNER);
const fraction = signal(FLOAT_DEFAULT);
let loaded = false;
/** Where the person left the window, as the Expo app kept it (the upgrade copies its keys). */
function loadPlace(): void {
  if (loaded) return;
  loaded = true;
  void Promise.all([Storage.getItem(FLOAT_CORNER_KEY), Storage.getItem(FLOAT_SIZE_KEY)])
    .then(([savedCorner, savedSize]) => {
      corner.set(parseFloatCorner(savedCorner));
      fraction.set(parseFloatFraction(savedSize));
    })
    .catch(() => undefined);
}

/**
 * The one floating window for every agent screen: Phone use, an agent's Linux display, the Mac. It owns
 * position, corners, size, the bar and the menu; what it shows is the hosted live view, which is also the
 * input surface, so only the bar drags. Mounted once above the app's sections by ScreenFloatHost.
 */
export function ScreenFloat(props: ScreenFloatProps) {
  loadPlace();
  const extra = () => props.extraHeight?.() ?? 0;
  const ratio = () => (props.ratio() > 0 ? props.ratio() : 9 / 20);
  const area = () => {
    const window = windowSize();
    const insets = safeArea();
    const keyboard = keyboardFrame().height;
    return {
      left: insets.left,
      top: insets.top + HEADER,
      right: window.width - insets.right,
      bottom: window.height - (keyboard > 0 ? keyboard : Math.max(insets.bottom, props.bottomInset?.() ?? 0)) - (props.typing?.() ? 0 : COMPOSER),
    };
  };
  // The picture has the screen's exact shape, so it fills its place; the window never reaches the composer or the keyboard.
  const picture = () => {
    const a = area();
    return floatSize(ratio(), fraction(), windowSize(), Math.max(0, a.bottom - a.top - 2 * EDGE - BAR - extra()));
  };
  const size = () => ({ width: picture().width, height: picture().height + BAR + extra() });

  // While a finger moves it, the window follows the finger; let go, it goes to the nearest corner.
  const drag = signal<{ dx: number; dy: number } | null>(null);
  const origin = () => {
    const o = floatOrigin(corner(), area(), size(), EDGE);
    const d = drag();
    return d ? { x: o.x + d.dx, y: o.y + d.dy } : o;
  };
  // Let go, the window stands at its new corner and glides there natively from where the finger left it
  // (with less motion asked for, it is there at once).
  const settle = createMotion();
  const bar = PanResponder.create({
    // The bar's buttons take a tap; a move of a few points is a drag, wherever it began on the bar.
    onMoveShouldSetPanResponderCapture: (_e, g) => Math.abs(g.dx) > 6 || Math.abs(g.dy) > 6,
    onPanResponderGrant: () => settle.reset(),
    onPanResponderMove: (_e, g) => drag.set({ dx: g.dx, dy: g.dy }),
    onPanResponderRelease: (_e, g) => {
      untracked(() => {
        const start = floatOrigin(corner(), area(), size(), EDGE);
        const left = { x: start.x + g.dx, y: start.y + g.dy };
        // A flick carries a little further than the finger.
        const center = { x: left.x + g.vx * 0.08 + size().width / 2, y: left.y + g.vy * 0.08 + size().height / 2 };
        const next = nearestFloatCorner(center, area());
        const end = floatOrigin(next, area(), size(), EDGE);
        settle.translateX.set(left.x - end.x);
        settle.translateY.set(left.y - end.y);
        drag.set(null);
        corner.set(next);
        void settle.play({ keyframes: { translateX: [left.x - end.x, 0], translateY: [left.y - end.y, 0] }, duration: SNAP_MS, easing: "easeOut" });
        void Storage.setItem(FLOAT_CORNER_KEY, next).catch(() => undefined);
      });
    },
    onPanResponderTerminate: () => drag.set(null),
  });

  // The handle in the inner corner resizes with one finger along the diagonal.
  let startWidth = 0;
  const handle = PanResponder.create({
    onStartShouldSetPanResponder: () => true,
    onPanResponderGrant: () => {
      startWidth = untracked(size).width;
    },
    onPanResponderMove: (_e, g) => {
      untracked(() => fraction.set(clampFloatFraction(resizedFloatWidth(startWidth, ratio(), { x: g.dx, y: g.dy }, corner()) / windowSize().width)));
    },
    onPanResponderRelease: () => void Storage.setItem(FLOAT_SIZE_KEY, String(untracked(fraction))).catch(() => undefined),
  });

  // A tap on the title goes to the next screen once no second tap follows; two quick taps go between the
  // default and the large size and stay on this screen.
  let lastTap = 0;
  let pendingCycle: ReturnType<typeof setTimeout> | null = null;
  const cancelCycle = () => {
    if (pendingCycle !== null) clearTimeout(pendingCycle);
    pendingCycle = null;
  };
  cleanup(cancelCycle);
  const tapTitle = () => {
    const now = Date.now();
    if (now - lastTap < DOUBLE_TAP_MS) {
      lastTap = 0;
      cancelCycle();
      const next = toggledFloatFraction(fraction());
      fraction.set(next);
      void Storage.setItem(FLOAT_SIZE_KEY, String(next)).catch(() => undefined);
      return;
    }
    lastTap = now;
    if (props.count() <= 1) return;
    cancelCycle();
    pendingCycle = setTimeout(() => {
      pendingCycle = null;
      props.onCycle();
    }, DOUBLE_TAP_MS);
  };

  const openMenu = async () => {
    const items = props.menu();
    const answer = await AlertDialog.show({
      title: props.title(),
      presentation: "actionSheet",
      actions: [...items.map((item) => ({ id: item.id, label: item.label, role: item.destructive ? ("destructive" as const) : undefined })), { id: "cancel", label: "Cancel", role: "cancel" as const }],
    }).catch(() => null);
    items.find((item) => item.id === answer?.action)?.run();
  };

  const cycles = () => props.count() > 1;
  // The handle sits in the corner opposite the one the window is anchored to, so dragging it outward grows the window.
  const handleLeft = () => corner().endsWith("right");
  const handleTop = () => corner().startsWith("bottom");

  // Until the window's size is known the float would have none: nothing to press, so nothing is drawn.
  const sized = computed(() => windowSize().width > 0);
  return <View class="absolute" style={{ left: 0, top: 0 }}>{sized() ? untracked(frame) : null}</View>;

  function frame() {
  return (
    <View
      testID="screen-float"
      accessibilityLabel={`Screen of ${props.title()}`}
      class="absolute overflow-hidden rounded-[10px] bg-card shadow-lg"
      style={{ left: origin().x, top: origin().y, width: size().width, height: size().height, transform: settle.transform }}
    >
      <View {...bar.panHandlers} class="flex-row items-center gap-0.5 pl-2.5 pr-0.5" style={{ height: BAR, backgroundColor: "rgba(24,24,24,0.94)" }}>
        <Button testID="screen-float-title" accessibilityLabel={cycles() ? `${props.title()}, ${props.index() + 1} of ${props.count()}, show the next` : props.title()} onPress={tapTitle} class="h-full flex-1 justify-center">
          <Text class="text-xs line-clamp-1" style={{ color: "#fff" }}>{cycles() ? `${props.title()}  ${props.index() + 1}/${props.count()}` : props.title()}</Text>
        </Button>
        {props.keyboard?.() ? (
          <Button testID="screen-float-keyboard" accessibilityLabel={props.keyboard!()!.open ? "Hide keyboard" : "Show keyboard"} onPress={() => props.keyboard?.()?.toggle()}
            class="size-7 items-center justify-center rounded-md" style={{ backgroundColor: props.keyboard!()!.open ? "rgba(255,255,255,0.22)" : "transparent" }}>
            <Icon name="keyboard" size={15} tone="canvas" />
          </Button>
        ) : null}
        <Button testID="screen-float-options" accessibilityLabel={`Options for ${props.title()}`} onPress={() => void openMenu()} class="size-7 items-center justify-center">
          <Icon name="more-horizontal" size={15} tone="canvas" />
        </Button>
        <Button testID="screen-float-open" accessibilityLabel="Open" onPress={props.onOpen} class="size-7 items-center justify-center">
          <Icon name="expand" size={15} tone="canvas" />
        </Button>
      </View>
      <View class="bg-popover" style={{ width: picture().width, height: picture().height + extra() }}>{props.children(picture)}</View>
      <View class="absolute inset-0 rounded-[10px] border border-border" style={{ pointerEvents: "none" }} />
      {props.attention?.() ? <AttentionRing /> : null}
      <View {...handle.panHandlers} testID="screen-float-resize" accessibilityLabel="Resize" class="absolute p-2"
        style={{ width: HANDLE, height: HANDLE, ...(handleLeft() ? { left: 0 } : { right: 0 }), ...(handleTop() ? { top: BAR } : { bottom: 0 }) }}>
        <View class={handleTop() ? "border-white/90" : "border-muted-foreground"} style={{
          width: 14, height: 14,
          ...(handleLeft() ? { borderLeftWidth: 3 } : { borderRightWidth: 3 }),
          ...(handleTop() ? { borderTopWidth: 3 } : { borderBottomWidth: 3 }),
        }} />
      </View>
    </View>
  );
  }
}

/** A ring in the seal colour that keeps breathing while something asks for the person, natively; with less motion asked for, it stays lit. */
function AttentionRing() {
  const breath = createMotion();
  effect(() => {
    if (reduceMotion()) {
      breath.reset();
      return;
    }
    void breath.play({ keyframes: { opacity: [1, 0.2] }, duration: BREATH_MS, easing: "easeInOut", iterations: "infinite", direction: "alternate" });
  });
  cleanup(() => breath.stop());
  return <View testID="screen-float-attention" class="absolute inset-0 rounded-[10px] border-[3px] border-seal" style={{ pointerEvents: "none", opacity: breath.opacity }} />;
}
