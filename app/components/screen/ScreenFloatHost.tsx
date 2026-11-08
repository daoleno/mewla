import React, { useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, PixelRatio, StyleSheet, View } from "react-native";
import { usePathname, useRouter } from "expo-router";
import { Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import { MewlaPhone, PhoneLiveView } from "../../modules/mewla-phone/src";
import { haltDisplay } from "../../services/display";
import { screenRequest } from "../../services/screen";
import type { StoredServer } from "../../services/storage";
import { useCurrentServer } from "../../store/currentServer";
import { usePhoneUse } from "../../store/phoneUse";
import { useWork } from "../../store/work";
import { useWorkers } from "../../store/workers";
import { isWorkerActivelyRunning } from "../../services/workerStatusPresentation";
import { floatPlaceholder, phoneAgentWorking, phoneMode } from "../phone/phoneSetup";
import { AppText, IconButton } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { LINUX_MODIFIERS, MAC_MODIFIERS, needsSoftKeyboard, RemoteView } from "./RemoteView";
import { ScreenFloat } from "./ScreenFloat";
import {
  PHONE_ROUTE,
  SCREEN_ROUTE,
  displayShown,
  displaySource,
  floatSources,
  macSource,
  phoneSource,
  pickSource,
  routeOwner,
  screenTarget,
  type ScreenSource,
} from "./screenFloatModel";
import { useAgentScreens } from "./useAgentScreens";
import { useRemoteScreen } from "./useRemoteScreen";

/** Frames the phone's small window asks for, and the width in steps so a resize does not ask again at every move. */
const PHONE_FPS = 4;
const FRAME_STEP = 120;
const FRAME_MIN = 240;
const FRAME_MAX = 720;
/** What the remote view's key bar takes under the picture while the system keyboard is up. */
const KEY_BAR = TouchTarget + 2 * Spacing.xs + 1;

/**
 * The floating live view of whichever agent screen the person is near: while the agent of the Session or
 * Work on screen uses the phone, its Linux display or the Mac, it floats over the page. It is mounted once
 * above navigation; each screen's own page is the full view and hides it there.
 */
export function ScreenFloatHost() {
  const pathname = usePathname();
  const router = useRouter();
  const phone = usePhoneUse();
  const { currentServer } = useCurrentServer();
  const { state: workerState } = useWorkers();
  const { state: workState } = useWork();
  const [picked, setPicked] = useState<string | null>(null);
  const [holding, setHolding] = useState<string | null>(null);
  const shown = useRef(new Set<string>());

  // Leaving the App from any page floats the phone agent's screen natively, not only from the Phone page.
  const status = phone.status;
  const arm = phoneAgentWorking(status);
  const w = status?.w ?? 0;
  const h = status?.h ?? 0;
  useEffect(() => {
    if (!arm || !MewlaPhone?.setPipAuto) return;
    MewlaPhone.setPipAuto(true, w, h);
    return () => MewlaPhone?.setPipAuto?.(false, w, h);
  }, [arm, w, h]);

  const serverId = currentServer?.id ?? "";
  const workers = useMemo(() => workerState.workers.filter((worker) => worker.serverId === serverId), [serverId, workerState.workers]);
  const owner = routeOwner(pathname, (id) => workState.byKey[`${serverId}:${id}`]?.frontmatter.worker_session);
  // Something could be using a screen while a Worker that may own one runs a turn.
  const mayUse = owner ? workers.some((worker) => worker.id === owner && isWorkerActivelyRunning(worker.status)) : workers.some((worker) => isWorkerActivelyRunning(worker.status));
  const screens = useAgentScreens(currentServer, mayUse || holding !== null);

  const all = useMemo(() => {
    const out: ScreenSource[] = [];
    const phoneOne = PhoneLiveView ? phoneSource(status, workers) : null;
    if (phoneOne) out.push(phoneOne);
    for (const display of screens.displays) {
      const worker = workers.find((candidate) => candidate.id === display.id);
      const key = `display:${display.id}`;
      if (displayShown(display, { running: worker ? isWorkerActivelyRunning(worker.status) : false, shown: shown.current.has(key), holding: holding === key })) {
        out.push(displaySource(display, worker?.name));
      }
    }
    for (const agent of screens.mac) out.push(macSource({ ...agent, name: agent.name || workers.find((candidate) => candidate.id === agent.worker)?.name }));
    return out;
  }, [holding, screens.displays, screens.mac, status, workers]);
  const sources = useMemo(() => floatSources(all, { pathname, owner }), [all, owner, pathname]);
  const source = pickSource(sources, picked);

  // A display that was up stays up between the agent's actions while its Worker runs.
  useEffect(() => {
    shown.current = new Set(sources.filter((s) => s.kind === "display").map((s) => s.key));
  }, [sources]);

  if (!source || !currentServer) return null;
  const index = sources.findIndex((s) => s.key === source.key);
  const cycle = () => setPicked(sources[(index + 1) % sources.length].key);
  const open = () => router.push(source.kind === "phone" ? PHONE_ROUTE : SCREEN_ROUTE);
  const shared = { count: sources.length, index, onCycle: cycle, onOpen: open };

  if (source.kind === "phone" && status) return <PhoneContent key={source.key} source={source} {...shared} />;
  return (
    <RemoteContent
      key={source.key}
      server={currentServer}
      source={source}
      onHold={(on) => setHolding((now) => (on ? source.key : now === source.key ? null : now))}
      onChanged={screens.refresh}
      {...shared}
    />
  );
}

interface FloatProps {
  source: ScreenSource;
  count: number;
  index: number;
  onCycle(): void;
  onOpen(): void;
}

/** Phone use: the native live view, watch only. Taking over is in the Phone page, which Take over opens. */
function PhoneContent({ source, count, index, onCycle, onOpen }: FloatProps) {
  const { colors } = useAppTheme();
  const phone = usePhoneUse();
  const status = phone.status;
  const mode = status ? phoneMode(status) : "idle";
  const handoff = mode === "handoff";
  const driving = mode === "driving";
  const placeholder = status ? floatPlaceholder(status) : null;
  const menu: ActionMenuItem[] = [
    handoff
      ? { key: "done", icon: "check", label: "Done", onPress: phone.handoffDone }
      : driving
        ? { key: "give-back", icon: "return-forward", label: "Give back", onPress: phone.giveBack }
        : { key: "take-over", icon: "hand", label: "Take over", onPress: () => { phone.takeOver(); onOpen(); } },
    { key: "stop", icon: "stop", label: "Stop", destructive: true, onPress: phone.stop },
  ];
  return (
    <ScreenFloat ratio={source.width / source.height} title={source.title} count={count} index={index} onCycle={onCycle} onOpen={onOpen} menu={menu} attention={handoff}>
      {(picture) => (
        <>
          <View style={StyleSheet.absoluteFill} pointerEvents="none">
            {PhoneLiveView ? (
              <PhoneLiveView
                style={StyleSheet.absoluteFill}
                fps={PHONE_FPS}
                maxWidth={Math.min(FRAME_MAX, Math.max(FRAME_MIN, Math.ceil((picture.width * PixelRatio.get()) / FRAME_STEP) * FRAME_STEP))}
              />
            ) : null}
          </View>
          {placeholder ? (
            <View style={[styles.cover, { backgroundColor: colors.bgSurface }]} pointerEvents="none" accessibilityLiveRegion="polite">
              {placeholder.failed ? null : <ActivityIndicator color={colors.textTertiary} />}
              {placeholder.text ? (
                <AppText variant="caption" tone={placeholder.failed ? "danger" : "secondary"} style={styles.coverText}>
                  {placeholder.text}
                </AppText>
              ) : null}
            </View>
          ) : null}
        </>
      )}
    </ScreenFloat>
  );
}

interface RemoteContentProps extends FloatProps {
  server: StoredServer;
  onHold(on: boolean): void;
  /** The agent's display or control changed here: read the computer again. */
  onChanged(): void;
}

/**
 * A Linux display or the Mac's screen: the same RemoteView as in Screen, the view being the input surface.
 * A display takes the person's clicks and keys (Hold and Stop in the menu); the Mac's is a watch, which
 * never takes the screen from the agent, and Take over stays in Screen.
 */
function RemoteContent({ server, source, count, index, onCycle, onOpen, onHold, onChanged }: RemoteContentProps) {
  const { colors } = useAppTheme();
  const target = useMemo(() => screenTarget(source), [source]);
  const { view, phase, holding, typing, setTyping, size, hold, reconnect, onEvent, onSend } = useRemoteScreen(server, target, source.key);
  const display = source.kind === "display";
  const live = phase === "live";

  useEffect(() => {
    onHold(holding);
  }, [holding]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => onHold(false), []); // eslint-disable-line react-hooks/exhaustive-deps

  const stop = async () => {
    try {
      if (display) await haltDisplay(server, source.owner ?? "");
      else await screenRequest(server, { action: "stop_agent", worker: source.owner ?? undefined });
    } catch {
      // The next read shows what the computer says.
    }
    onChanged();
  };
  const menu: ActionMenuItem[] = [
    ...(display && live
      ? [holding
        ? { key: "release", icon: "play", label: "Let the agent go on", onPress: () => hold(false) } as const
        : { key: "hold", icon: "hand", label: "Hold the agent", onPress: () => hold(true) } as const]
      : []),
    { key: "stop", icon: "stop", label: "Stop", destructive: true, accessibilityLabel: `Stop ${source.title}`, onPress: () => void stop() },
    ...(live && !needsSoftKeyboard() ? [{ key: "full", icon: "expand", label: "Full size", onPress: () => view.current?.fullSize() } as const] : []),
    ...(phase === "lost" ? [{ key: "reconnect", icon: "refresh", label: "Reconnect", onPress: reconnect } as const] : []),
  ];
  const keyboard = display && live && needsSoftKeyboard();
  const shape = size ?? source;
  return (
    <ScreenFloat
      ratio={shape.width / shape.height}
      title={source.title}
      count={count}
      index={index}
      onCycle={onCycle}
      onOpen={onOpen}
      menu={menu}
      onKeyboard={keyboard ? () => view.current?.toggleKeyboard() : undefined}
      keyboardOpen={typing}
      extraHeight={typing ? KEY_BAR : 0}
      typing={typing}
      attention={holding}
    >
      {(picture) => (
        <>
          <RemoteView
            ref={view}
            backgroundColor={colors.bgElevated}
            accessibilityLabel={`${display ? "Display" : "Screen"} of ${source.title}`}
            align="center"
            modifiers={display ? LINUX_MODIFIERS : MAC_MODIFIERS}
            viewStyle={{ width: picture.width, height: picture.height }}
            onSend={onSend}
            onEvent={onEvent}
            onKeyboardChange={setTyping}
          />
          {phase !== "live" ? (
            <View style={[styles.cover, styles.coverRemote, { height: picture.height }]} pointerEvents={phase === "lost" ? "box-none" : "none"}>
              {phase === "connecting" ? <ActivityIndicator color={colors.textTertiary} /> : <IconButton icon="refresh" size={36} accessibilityLabel="Reconnect" onPress={reconnect} />}
            </View>
          ) : null}
        </>
      )}
    </ScreenFloat>
  );
}

const styles = StyleSheet.create({
  cover: { position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center", gap: 8, padding: 12 },
  coverRemote: { bottom: undefined },
  coverText: { textAlign: "center" },
});
