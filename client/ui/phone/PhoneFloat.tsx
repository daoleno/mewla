import { safeArea, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { floatPlaceholder, phoneFloat, phoneMode } from "../../core/phone/setup";
import { Icon } from "../more/kit/Icon";
import { Spinner } from "../more/kit/kit";
import { PhoneLiveView } from "./contract";
import { phoneControl, phonePageOpen, phoneStatus } from "./state";

/** What the small window asks of the shared frame stream: few frames, about its own width. */
const FLOAT_FPS = 4;
const FLOAT_WIDTH = 132;
const FRAME_WIDTH = 360;

/**
 * The floating control over any page while an agent works this phone: its live picture (watch only;
 * a tap opens the Phone page), Take over / Give back / Done, and Stop. Hidden on the Phone page,
 * which is the full view. Mount it once above the app's navigation.
 */
export function PhoneFloat(props: { onOpen: () => void; bottom?: number }) {
  const shape = () => (phonePageOpen() ? null : phoneFloat(phoneStatus()));
  const mode = () => (phoneStatus() ? phoneMode(phoneStatus()!) : "idle");
  const height = () => {
    const s = shape();
    return s ? Math.round(Math.min(Math.max((FLOAT_WIDTH * s.height) / s.width, 120), 300)) : 0;
  };
  const placeholder = () => (phoneStatus() ? floatPlaceholder(phoneStatus()!) : null);
  const title = () => shape()?.title ?? "Phone";
  const primary = () =>
    mode() === "handoff"
      ? { id: "phone-float-done", label: "Done", run: () => phoneControl("handoffDone") }
      : mode() === "driving"
        ? { id: "phone-float-give-back", label: "Give back", run: () => phoneControl("giveBack") }
        : {
            id: "phone-float-take-over",
            label: "Take over",
            run: () => {
              phoneControl("takeOver");
              props.onOpen();
            },
          };

  // Made once per appearance: a status change redraws what it changes, never the live picture.
  const float = () => (
    <View
      testID="phone-float"
      class={`absolute right-3 overflow-hidden rounded-2xl border bg-card shadow-lg ${mode() === "handoff" ? "border-seal" : "border-border"}`}
      style={{ width: FLOAT_WIDTH, bottom: (props.bottom ?? 96) + safeArea().bottom }}
    >
      <Button testID="phone-float-open" accessibilityLabel={`Open Phone use: ${title()}`} onPress={props.onOpen} style={{ height: height() }}>
        <View class="flex-1 bg-card" style={{ pointerEvents: "none" }}>
          <PhoneLiveView class="flex-1" fps={FLOAT_FPS} maxWidth={FRAME_WIDTH} />
          {placeholder() ? (
            <View class="absolute inset-0 items-center justify-center gap-2 bg-card p-3">
              {placeholder()!.failed ? null : <Spinner size={18} tone="faint" />}
              {placeholder()!.text ? (
                <Text class={`text-center text-xs ${placeholder()!.failed ? "text-destructive" : "text-muted-foreground"}`}>{placeholder()!.text!}</Text>
              ) : null}
            </View>
          ) : null}
        </View>
      </Button>
      <Text class="px-2 pt-1.5 text-xs font-medium line-clamp-1">{title()}</Text>
      <View class="flex-row gap-1.5 p-2">
        <Button testID={primary().id} accessibilityLabel={primary().label} onPress={() => primary().run()} class="h-8 flex-1 items-center justify-center rounded-full bg-primary active:opacity-70">
          <Text class="text-xs font-semibold text-primary-foreground">{primary().label}</Text>
        </Button>
        <Button testID="phone-float-stop" accessibilityLabel={`Stop ${title()}`} onPress={() => phoneControl("stop")} class="size-8 items-center justify-center rounded-full bg-secondary active:opacity-70">
          <Icon name="stop" size={14} tone="seal" />
        </Button>
      </View>
    </View>
  );
  return <>{((shown) => untracked(() => (shown ? float() : null)))(shape() !== null)}</>;
}
