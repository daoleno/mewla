import { cleanup, effect, Scroll, signal, Text, untracked, View, type JSX } from "nsh";
import { Button, NavPage, Switch } from "nsh/base";
import { pageStatusBar } from "../shell/NavPage";
import { phoneMode, phoneSetupPlan, phoneSteps, type PhoneMode, type PhoneStepId } from "../../core/phone/setup";
import { color } from "../kit/colors";
import { Icon, type IconName } from "../kit/Icon";
import { Notice, Row, Section, Spinner } from "../kit/kit";
import { PhoneLiveView } from "./contract";
import {
  allowPhoneAccessibility,
  floatPhone,
  openPhoneScreen,
  pairPhone,
  phoneAvailable,
  phoneControl,
  phoneError,
  phonePairing,
  phoneStatus,
  startPhoneHelper,
  turnPhoneOff,
  turnPhoneOn,
  watchPhoneSetup,
} from "./state";

/**
 * Phone use (Settings → Labs → Phone use, mewla://phone): the switch, then the setup until it works,
 * then the agent's own screen with Take over and Stop one tap away.
 */
export function PhonePage() {
  cleanup(watchPhoneSetup());
  // Once paired, the helper starts by itself: nothing for the person to press.
  let asked = "";
  effect(() => {
    const s = phoneStatus();
    const key = s ? `${s.enabled}:${s.paired}:${s.helper}` : "";
    untracked(() => {
      if (key === asked) return;
      asked = key;
      if (s?.enabled && s.paired && !s.helper) void startPhoneHelper();
    });
  });

  const busy = signal(false);
  const toggle = (on: boolean) => {
    if (busy()) return;
    busy.set(true);
    void (on ? turnPhoneOn() : turnPhoneOff()).catch(() => {}).finally(() => busy.set(false));
  };

  /** Which body: one per kind, so the live picture is not made again between watching and driving. */
  const view = (): "none" | "off" | "setup" | "live" => {
    const s = phoneStatus();
    if (!phoneAvailable() || !s) return "none";
    if (!s.enabled) return "off";
    return phoneMode(s) === "setup" ? "setup" : "live";
  };
  const body = (key: ReturnType<typeof view>): JSX.Element => {
    if (key === "none") return <View />;
    if (key === "off") return <Off onTurnOn={() => toggle(true)} busy={busy()} />;
    if (key === "setup") return <Setup />;
    return <Live />;
  };

  return (
    <NavPage
      title="Phone use"
      statusBar={pageStatusBar()}
      testID="phone-page"
      headerRight={
        phoneAvailable() ? (
          <View class="flex-row items-center gap-2 pr-1">
            {busy() ? <Spinner size={16} /> : null}
            <Switch
              testID="phone-switch"
              accessibilityLabel="Phone use"
              checked={phoneStatus()?.enabled === true}
              disabled={busy()}
              onChange={({ checked }) => toggle(checked)}
              class="disabled:opacity-50"
              trackClass="h-[30px] w-[50px] rounded-full bg-secondary border border-border checked:bg-primary checked:border-primary"
              thumbClass="m-[2px] size-[24px] rounded-full bg-card shadow-sm checked:translate-x-[20px]"
            />
          </View>
        ) : undefined
      }
    >
      <View class="flex-1 bg-background">{((key) => untracked(() => body(key)))(view())}</View>
    </NavPage>
  );
}

function Off(props: { onTurnOn: () => void; busy: boolean }) {
  return (
    <View class="flex-1 items-center justify-center gap-6 px-6" testID="phone-off">
      <Icon name="phone" size={44} tone="faint" />
      <BigButton testID="phone-turn-on" label="Turn on" filled busy={props.busy} onPress={props.onTurnOn} />
    </View>
  );
}

function Setup() {
  const plan = () => phoneSetupPlan(phoneStatus()!, phonePairing());

  // The guided setup runs in the background of this page: Mewla opens Settings, taps through it and
  // brings the person back here; this page only starts it and shows where it is.
  const run = (id: PhoneStepId) => {
    if (id === "developer") openPhoneScreen("developer");
    else if (id === "wifi" || id === "wireless") openPhoneScreen("wireless");
    else if (id === "pair") {
      openPhoneScreen("wireless");
      void pairPhone();
    } else void startPhoneHelper();
  };
  const primary = () => {
    const action = plan().action;
    if (action === "allow") void allowPhoneAccessibility();
    else if (action === "setup") void pairPhone();
    else if (action === "wifi" || action === "wireless") openPhoneScreen("wireless");
    else void startPhoneHelper();
  };
  const working = () => phonePairing() || phoneStatus()?.guide != null;

  return (
    <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="px-4 pb-6" testID="phone-setup">
      <View class="w-full max-w-[720px] self-center">
        <View class="items-center py-7">
          <Icon name="phone" size={40} tone="ink" />
        </View>
        <Section>
          {phoneSteps(phoneStatus()!).map((step) => {
            const current = plan().current === step.id;
            const spinning = current && !step.done && (working() || step.id === "helper");
            return (
              <Row
                testID={`phone-step-${step.id}`}
                title={step.title}
                subtitle={current ? plan().note ?? step.hint : undefined}
                onPress={step.done ? undefined : () => run(step.id)}
                trailing={
                  step.done ? (
                    <Icon name="check-circle-fill" size={22} tone="ready" />
                  ) : spinning ? (
                    <Spinner size={20} tone="faint" />
                  ) : (
                    <Icon name="circle" size={22} tone="faint" />
                  )
                }
              />
            );
          })}
        </Section>
        <View class="mt-8 gap-2">
          <BigButton
            testID="phone-setup-primary"
            label={plan().label}
            filled
            busy={working() && plan().action === "setup"}
            onPress={primary}
          />
          {plan().action === "allow" && phoneStatus()?.restricted ? (
            <Button testID="phone-app-info" onPress={() => openPhoneScreen("app")} class="items-center py-3 active:opacity-60">
              <Text class="text-base font-medium">Open App info</Text>
            </Button>
          ) : null}
        </View>
        {phoneError() ? <Notice tone="failed" title={phoneError()} /> : null}
      </View>
    </Scroll>
  );
}

function Live() {
  const mode = () => phoneMode(phoneStatus()!) as Exclude<PhoneMode, "setup">;
  const driving = () => mode() === "driving" || mode() === "handoff";
  const ratio = () => {
    const s = phoneStatus()!;
    return s.w > 0 && s.h > 0 ? s.w / s.h : 9 / 20;
  };
  const title = () => {
    const s = phoneStatus()!;
    const m = mode();
    return m === "handoff" ? s.handoff ?? "" : m === "stopped" ? "Stopped" : m === "idle" ? "Ready" : s.agent ?? "";
  };
  const dot = () => color(mode() === "idle" ? "ready" : driving() ? "seal" : "running");
  const showPicture = () => (phoneStatus()?.w ?? 0) > 0 && mode() !== "idle";

  return (
    <View class="flex-1" testID="phone-live">
      <View class="flex-row items-center justify-center gap-2 px-4 py-3">
        {/* A long name ("Phonebot2 (mh-…)") shrinks and ends in "…" on the right: unshrunk, the centred row cut it at the left (phone 15). */}
        <Text testID="phone-live-title" class="min-w-0 shrink text-lg font-semibold line-clamp-1">{title()}</Text>
        <View class="size-2 rounded-full" style={{ backgroundColor: dot() }} />
        {mode() === "watching" || mode() === "driving" ? (
          <Button testID="phone-pip" accessibilityLabel="Float" hitSlop={8} onPress={floatPhone} class="p-2 active:opacity-60">
            <Icon name="expand" size={18} />
          </Button>
        ) : null}
      </View>
      <View class="flex-1 items-center justify-center px-4">
        {showPicture() ? (
          <View class="w-full max-h-full overflow-hidden rounded-[20px] border border-border bg-card" style={{ aspectRatio: ratio() }}>
            <PhoneLiveView class="flex-1" interactive={driving()} accessibilityLabel="The agent's screen" />
          </View>
        ) : (
          <Icon name="check-circle" size={56} tone="faint" />
        )}
      </View>
      <View class="flex-row gap-3 px-4 py-3">
        {mode() === "watching" ? (
          <>
            <BigButton testID="phone-take-over" icon="hand" label="Take over" filled grow onPress={() => phoneControl("takeOver")} />
            <BigButton testID="phone-stop" icon="stop" label="Stop" destructive onPress={() => phoneControl("stop")} />
          </>
        ) : null}
        {mode() === "driving" ? (
          <>
            <BigButton testID="phone-give-back" icon="return-forward" label="Give back" filled grow onPress={() => phoneControl("giveBack")} />
            <BigButton testID="phone-stop" icon="stop" label="Stop" destructive onPress={() => phoneControl("stop")} />
          </>
        ) : null}
        {mode() === "handoff" ? (
          <>
            <BigButton testID="phone-done" icon="check" label="Done" filled grow onPress={() => phoneControl("handoffDone")} />
            <BigButton testID="phone-stop" icon="stop" label="Stop" destructive onPress={() => phoneControl("stop")} />
          </>
        ) : null}
        {mode() === "stopped" ? (
          <BigButton testID="phone-resume" icon="play" label="Resume" filled grow onPress={() => phoneControl("giveBack")} />
        ) : null}
      </View>
    </View>
  );
}

/** The page's one big action, filled in ink, or a quiet one; Stop in the seal's red. */
function BigButton(props: {
  label: string;
  onPress: () => void;
  icon?: IconName;
  filled?: boolean;
  destructive?: boolean;
  grow?: boolean;
  busy?: boolean;
  testID?: string;
}) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.label}
      disabled={props.busy}
      onPress={props.onPress}
      class={`h-12 flex-row items-center justify-center gap-2 rounded-full px-6 active:opacity-70 disabled:opacity-60 ${props.grow ? "flex-1" : ""} ${props.filled ? "bg-primary" : "bg-secondary"}`}
    >
      {props.busy ? <Spinner size={18} tone={props.filled ? "canvas" : "soft"} /> : props.icon ? <Icon name={props.icon} size={18} tone={props.filled ? "canvas" : props.destructive ? "seal" : "ink"} /> : null}
      <Text class={`text-base font-semibold ${props.filled ? "text-primary-foreground" : props.destructive ? "text-destructive" : ""}`}>{props.label}</Text>
    </Button>
  );
}
