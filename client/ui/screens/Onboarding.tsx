import { cleanup, Clipboard, createMotion, effect, host, openUrl, platform, reduceMotion, safeArea, Scroll, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { NavPage } from "../shell/NavPage";
import { BackHandler } from "nsh/mobile";
import {
  INSTALL_COMMAND,
  INSTALL_NOTE,
  OTHER_WAYS_URL,
  START_COMMAND,
  START_NOTE,
  WELCOME_LEDE,
  WELCOME_TITLE,
  belowHero,
  catBoxForStep,
  catClipFor,
  computerName,
  connectedCopy,
  headerHeight,
  heroCatBox,
  heroZoneWidth,
  isCompactFrame,
  previousOnboardingStep,
  sealShown,
  type CatBox,
  type Frame,
  type OnboardingStep,
} from "../../core/onboarding";
import { Icon, type IconName } from "../icons/Icon";
import { Caption, DisplayTitle, Field, Spinner } from "../kit/kit";
import { color } from "../kit/colors";
import { restorePet } from "../pets/pets";
import { justPaired, pairFromField, pairFromPicture, pairingBusy, pairingIssue, pairingStatus, pairings, pasteFromClipboard } from "../pairing";
import { PetSprite } from "../pets/PetSprite";
import { connection, problem, resumeConnection, servers } from "../session";
import { probeComputer } from "../more/settings/computers";
import { OnboardingScanner, scannerSearching } from "./OnboardingScanner";

export { pairFromText } from "../pairing";

/**
 * First run: Welcome → Install → Scan → Connected, with Brain's cat hopping between the hero spot
 * and the corner. Also the connected step after any later pairing (Settings, an opened link):
 * `justPaired` holds the computer until the person goes on.
 */
export function Onboarding(props: { onContinue(): void; onSettings(): void }) {
  restorePet();
  const step = signal<OnboardingStep>(justPaired() ? "connected" : "welcome");
  const cameFrom = signal<OnboardingStep | null>(null);
  const size = signal<{ width: number; height: number } | null>(null);
  const go = (next: OnboardingStep) => {
    cameFrom.set(step());
    step.set(next);
  };
  effect(() => {
    if (justPaired()) step.set("connected");
  });
  onBackPress(() => {
    // A code that is pairing is not left half done.
    if (pairingBusy()) return true;
    const previous = previousOnboardingStep(step(), cameFrom());
    if (!previous) return false;
    cameFrom.set(null);
    step.set(previous);
    return true;
  });

  const frame = (): Frame | null => {
    const s = size();
    return s ? { ...s, top: safeArea().top, bottom: safeArea().bottom } : null;
  };
  const compact = () => (frame() ? isCompactFrame(frame()!) : false);
  // The computer as saved now: one paired by its address takes the name it reports (asked here, as Settings does).
  const server = () => {
    const paired = justPaired();
    return paired ? servers().find((s) => s.id === paired.id) ?? paired : null;
  };
  effect(() => {
    const paired = justPaired();
    if (paired) untracked(() => void probeComputer(paired).catch(() => {}));
  });
  const name = () => computerName(server()?.name);
  const finish = (then: () => void) => {
    justPaired.set(null);
    then();
  };

  return (
    <View testID="onboarding" class="flex-1 bg-background"
      style={{ paddingTop: safeArea().top, paddingBottom: safeArea().bottom, paddingLeft: safeArea().left, paddingRight: safeArea().right }}
      onLayout={({ bounds }) => {
        const { width, height } = bounds.size;
        const s = size();
        if (s?.width !== width || s?.height !== height) size.set({ width, height });
      }}>
      {step() === "welcome" ? (
        <HeroStep frame={frame()} compact={compact()} center={
          <>
            <Text role="header" class="text-center text-display font-bold tracking-display text-foreground">{WELCOME_TITLE.replace(". ", ".\n")}</Text>
            <Text class="max-w-[320px] text-center text-body text-muted-foreground">{WELCOME_LEDE}</Text>
          </>
        } actions={
          <>
            <FilledButton testID="set-up" label="Set up my computer" onPress={() => go("install")} />
            <PlainButton testID="scan-now" label="Mewla already running? Scan its code" onPress={() => go("scan")} />
          </>
        } />
      ) : step() === "install" ? (
        <SplitStep compact={compact()} content={
          <>
            <StepHeader frame={frame()} compact={compact()} number={1} place="On your computer" title="Install Mewla" />
            <CommandCard />
            <Text class="text-body text-muted-foreground">
              Then run <Text class="font-mono text-code text-foreground">{START_COMMAND}</Text>. {START_NOTE}
            </Text>
          </>
        } actions={<FilledButton testID="next-scan" label="Next: scan the code" onPress={() => go("scan")} />} />
      ) : step() === "scan" ? (
        <SplitStep compact={compact()} content={
          <>
            <StepHeader frame={frame()} compact={compact()} number={2} place="On this phone" title="Scan the code in your terminal" />
            <OnboardingScanner compact={compact()} />
            <ScanStatus />
          </>
        } actions={<PairAlternatives />} />
      ) : (
        <HeroStep frame={frame()} compact={compact()} live center={
          <>
            <Text testID="connected-title" role="header" class="text-center text-display font-bold tracking-display text-foreground">
              {connectedCopy(connection(), name(), problem()).title}
            </Text>
            {/* The pairing's success is the `ok` dot before "Connected to …" (SPEC §10.4: the scan's success is ok). */}
            <View class="max-w-[320px] flex-row items-center justify-center gap-2">
              {connection() === "connected" ? <View testID="connected-ok-dot" class="size-2 rounded-full" style={{ backgroundColor: color("ok") }} /> : null}
              <Text class="shrink text-center text-body text-muted-foreground">{connectedCopy(connection(), name(), problem()).lede}</Text>
            </View>
            {connection() === "connecting" || connection() === "idle" ? <Spinner /> : null}
          </>
        } actions={
          <>
            {connection() === "connected" ? <FilledButton testID="open-brain" icon="arrow-forward" label="Open Brain" onPress={() => finish(props.onContinue)} /> : null}
            {connection() === "waiting" ? <FilledButton testID="retry" icon="refresh" label="Retry connection" onPress={() => resumeConnection()} /> : null}
            {connection() !== "connected" ? <PlainButton testID="server-settings" icon="settings" label="Server settings" onPress={() => finish(props.onSettings)} /> : null}
          </>
        } />
      )}
      {frame() ? <Cat step={step()} frame={frame()!} /> : null}
    </View>
  );
}

/** Settings → "Pair a computer": the scan step on its own page; the connected step follows a pairing. `onScreen`: whether the page is shown (its camera stops when not). */
export function PairPage(props: { onPaired?(): void; onScreen?: () => boolean }) {
  const before = untracked(pairings);
  effect(() => {
    if (pairings() !== before) untracked(() => props.onPaired?.());
  });
  // A code that is pairing is not left half done.
  onBackPress(() => pairingBusy());
  // A browser has no camera reader: it pastes the link.
  const camera = platform.os !== "web";
  const link = signal("");
  const pair = () => void pairFromField(link()).then(() => {
    if (!pairingIssue()) link.set("");
  });
  return (
    <NavPage title="Pair a computer">
      {/* The field keeps its focus after the keyboard goes (Back): a tap on Pair still presses Pair. */}
      {/* The step flow's 480 column: a display title, one sentence, the finder, the field, the buttons ink last, the quiet ways out. */}
      <Scroll class="flex-1 bg-background" contentClass="px-4 pb-8 pt-2" keyboardShouldPersistTaps="handled">
        <View class="w-full max-w-[480px] gap-4 self-center">
          <DisplayTitle title="Pair a computer" />
          <View class="-mt-2 px-1"><Text class="text-body text-muted-foreground">
            {camera ? "On your computer, run mewla pair, then scan the code it shows, or paste the link it prints." : "On your computer, run mewla pair --details, copy the pairing link, then paste it here."}
          </Text></View>
          {camera ? <OnboardingScanner onScreen={props.onScreen} /> : null}
          <ScanStatus />
          <View class="overflow-hidden rounded-(--radius-group) bg-card">
            <Field testID="pair-link" label="Pairing link" mono multiline keyboardType="url" value={link()} disabled={pairingBusy()}
              placeholder="mewla://settings?p=… or https://…/#pair=…" onChange={(value) => link.set(value)} onSubmit={pair} />
          </View>
          <View class="gap-2">
            {camera ? <OutlinedButton testID="pick-qr-image" icon="image" label="Pick QR image" disabled={pairingBusy()} onPress={() => void pairFromPicture()} /> : null}
            <FilledButton testID="pair-link-submit" label="Pair" disabled={pairingBusy()} onPress={pair} />
          </View>
          <OtherWays />
        </View>
      </Scroll>
    </NavPage>
  );
}

/**
 * Android's Back while this component lives (iOS has no Back button and accepts the listener).
 * A browser has its own back: NSH's web runtime has no `BackHandler.on`, so nothing listens there.
 */
function onBackPress(listener: () => boolean | void): void {
  if (platform.os === "web") return;
  cleanup(BackHandler.on("press", listener));
}

/** Welcome and connected: the cat above the text, or beside it when the window is short. */
function HeroStep(props: { frame: Frame | null; compact: boolean; center: JSX.Element; actions: JSX.Element; live?: boolean }) {
  const fade = createMotion({ enter: "fadeIn", delay: 140, duration: 240 });
  // Components run once: the layout is chosen inside the JSX, so a rotation chooses again.
  return (
    <View class="flex-1">
      {props.compact ? (
        <View testID="step-compact" class="flex-1 pb-2 pl-0 pr-6" style={{ marginLeft: props.frame ? heroZoneWidth(props.frame) : 0, ...fade.style }}>
          <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="grow justify-center gap-5 py-3">
            <View class="items-center gap-3" android={{ accessibilityLiveRegion: props.live ? "polite" : "none" }}>{props.center}</View>
            <View class="w-full max-w-[480px] gap-2 self-center">{props.actions}</View>
          </Scroll>
        </View>
      ) : (
        <View testID="step-tall" class="w-full max-w-[480px] flex-1 self-center px-6 pb-4" style={{ paddingTop: props.frame ? belowHero(props.frame) : 260, ...fade.style }}>
          <View class="flex-1 items-center gap-3" android={{ accessibilityLiveRegion: props.live ? "polite" : "none" }}>{props.center}</View>
          <View class="gap-2">{props.actions}</View>
        </View>
      )}
    </View>
  );
}

/** Install and scan: the content scrolls, the buttons stay in reach (beside it when the window is short). */
function SplitStep(props: { compact: boolean; content: JSX.Element; actions: JSX.Element }) {
  const fade = createMotion({ enter: "fadeIn", delay: 140, duration: 240 });
  return (
    <View class="flex-1">
      {props.compact ? (
        <View testID="split-compact" class="flex-1 flex-row gap-6 px-6 pb-2" style={fade.style}>
          <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="gap-4 pb-4">{props.content}</Scroll>
          <View class="w-[220px] justify-end gap-2">{props.actions}</View>
        </View>
      ) : (
        <View testID="split-tall" class="w-full max-w-[480px] flex-1 self-center px-6 pb-4" style={fade.style}>
          <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="gap-4 pb-4">{props.content}</Scroll>
          <View class="gap-2">{props.actions}</View>
        </View>
      )}
    </View>
  );
}

function StepHeader(props: { frame: Frame | null; compact: boolean; number: number; place: string; title: string }) {
  return (
    <View class={props.compact ? "gap-2.5 pb-1.5 pt-3" : "justify-end gap-2.5 pb-1.5 pr-[100px] pt-[22px]"}
      style={{ minHeight: props.frame && !props.compact ? headerHeight(props.frame) : undefined }}>
      {/* The eyebrow (SPEC §15 Step flow): which step of the two, and where it happens. */}
      <Caption testID="step-eyebrow" text={`Step ${props.number} of 2 · ${props.place}`} />
      <Text role="header" class="text-display font-bold tracking-display text-foreground">{props.title}</Text>
    </View>
  );
}

/** The one command, with Copy and Send to computer (the system share sheet). */
function CommandCard() {
  const copied = signal(false);
  return (
    <View class="gap-3 rounded-(--radius-group) bg-card p-4">
      <Text testID="install-command" style={{ userSelect: "text" }} class="font-mono text-code text-foreground">{INSTALL_COMMAND}</Text>
      <Text class="text-caption text-muted-foreground">{INSTALL_NOTE}</Text>
      <View class="flex-row flex-wrap gap-2">
        <TintedButton testID="copy-command" icon={copied() ? "check" : "copy"} label={copied() ? "Copied" : "Copy"}
          onPress={() => void Clipboard.writeText(INSTALL_COMMAND).then(() => { copied.set(true); setTimeout(() => copied.set(false), 1_600); })} />
        {host.available() ? (
          <TintedButton testID="share-command" icon="share" label="Send to computer" onPress={() => void host.call("app.share", { text: INSTALL_COMMAND }).catch(() => {})} />
        ) : null}
      </View>
    </View>
  );
}

/** Under the finder: what the camera is doing, or why the last code did not pair. */
function ScanStatus() {
  return (
    <View class="gap-2" android={{ accessibilityLiveRegion: "polite" }}>
      <View class="min-h-6 flex-row items-center justify-center gap-2">
        {pairingBusy() ? <Spinner size={16} /> : null}
        {pairingBusy() || scannerSearching() ? <Text testID="scan-status" class="text-sub text-muted-foreground">{pairingBusy() ? pairingStatus() || "Pairing…" : "Looking for a Mewla code…"}</Text> : null}
      </View>
      {pairingIssue() ? (
        // The issue card, above the ways out; the finder keeps scanning.
        <View testID="pair-issue" class="gap-1 rounded-(--radius-group) bg-card px-4 py-3.5">
          <Text class="text-body font-semibold">{pairingIssue()!.title}</Text>
          <Text style={{ userSelect: "text" }} class="text-sub text-muted-foreground">{pairingIssue()!.detail}</Text>
        </View>
      ) : null}
    </View>
  );
}

/** Paste and the docs: pairing never depends on the camera. */
function PairAlternatives() {
  return (
    <>
      <PlainButton testID="paste" icon="clipboard" label="Paste a pairing link" disabled={pairingBusy()} onPress={() => void pasteFromClipboard()} />
      <OtherWays />
    </>
  );
}

function OtherWays() {
  return (
    <Button testID="other-ways" role="link" accessibilityLabel="Other ways to connect" onPress={() => void openUrl(OTHER_WAYS_URL)}
      class="min-h-11 flex-row items-center justify-center gap-1.5 active:opacity-70">
      <Text class="text-sub font-semibold text-muted-foreground">Other ways to connect</Text>
      <Icon name="open-external" size={14} />
    </Button>
  );
}

const CAT_BASE = 164;

/** Brain's cat: one image that moves between the hero spot and the corner; the seal stamps in behind it. */
function Cat(props: { step: OnboardingStep; frame: Frame }) {
  const box = (): CatBox => catBoxForStep(props.step, props.frame);
  const hero = () => heroCatBox(props.frame);
  const clip = () => catClipFor(props.step, pairingBusy(), justPaired() ? connection() : undefined);
  const seal = () => sealShown(props.step, justPaired() ? connection() : undefined);
  const move = () => (reduceMotion() ? 0 : 440);
  return (
    <>
      <View
        style={{
          position: "absolute", pointerEvents: "none", left: hero().cx - hero().size / 2, top: hero().cy - hero().size / 2, width: hero().size, height: hero().size,
          opacity: seal() ? 1 : 0, transform: [{ scale: seal() ? 1 : 1.5 }],
          transitionProperty: "opacity, transform", transitionDuration: reduceMotion() ? 0 : 260, transitionDelay: seal() && !reduceMotion() ? 380 : 0,
        }}>
        <PetSprite state={() => "homeless"} size={hero().size} />
      </View>
      {/* Drawn at one size and moved by a transform, so the move between spots runs natively. */}
      <View testID="cat"
        style={{
          position: "absolute", pointerEvents: "none", left: 0, top: 0, width: CAT_BASE, height: CAT_BASE,
          transform: [{ translateX: box().cx - CAT_BASE / 2 }, { translateY: box().cy - CAT_BASE / 2 }, { scale: box().size / CAT_BASE }],
          transitionProperty: "transform", transitionDuration: move(), transitionTimingFunction: "ease-in-out",
        }}>
        <PetSprite state={clip} size={CAT_BASE} />
      </View>
    </>
  );
}

function FilledButton(props: { testID: string; label: string; icon?: IconName; disabled?: boolean; onPress(): void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} disabled={props.disabled} accessibilityLabel={props.label}
      class="h-[52px] flex-row items-center justify-center gap-2 rounded-2xl bg-primary px-5 active:opacity-70 disabled:opacity-40">
      <Text class="text-body font-semibold text-primary-foreground line-clamp-1">{props.label}</Text>
      {props.icon ? <Icon name={props.icon} color="primary-foreground" size={18} /> : null}
    </Button>
  );
}

function OutlinedButton(props: { testID: string; label: string; icon?: IconName; disabled?: boolean; onPress(): void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} disabled={props.disabled} accessibilityLabel={props.label}
      class="h-[52px] flex-row items-center justify-center gap-2 rounded-2xl bg-card px-5 active:opacity-70 disabled:opacity-40">
      {props.icon ? <Icon name={props.icon} color="foreground" size={18} /> : null}
      <Text class="text-body font-semibold text-foreground line-clamp-1">{props.label}</Text>
    </Button>
  );
}

/** A quiet way out (SPEC §15 Step flow): a text button under the actions. */
function PlainButton(props: { testID: string; label: string; icon?: IconName; disabled?: boolean; onPress(): void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} disabled={props.disabled} accessibilityLabel={props.label}
      class="min-h-11 flex-row items-center justify-center gap-2 px-4 py-2.5 active:opacity-70 disabled:opacity-40">
      {props.icon ? <Icon name={props.icon} color="muted-foreground" size={16} /> : null}
      <Text class="text-center text-sub font-semibold text-muted-foreground">{props.label}</Text>
    </Button>
  );
}

function TintedButton(props: { testID: string; label: string; icon: IconName; onPress(): void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} accessibilityLabel={props.label}
      hitSlop={4}
      class="h-9 flex-row items-center gap-1.5 rounded-full bg-muted px-3.5 active:opacity-70">
      <Icon name={props.icon} color="foreground" size={16} />
      <Text class="text-sub font-semibold text-foreground">{props.label}</Text>
    </Button>
  );
}
