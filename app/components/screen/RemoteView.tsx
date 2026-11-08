import React, { forwardRef, useCallback, useImperativeHandle, useRef, useState } from "react";
import { Platform, Pressable, ScrollView, StyleSheet, TextInput, View, type StyleProp, type TextInputInstance, type ViewStyle } from "react-native";
import { ContinuousCorners, Radii, Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { ScreenViewerHost } from "./ScreenViewerHost";
import {
  activeModifiers,
  KEYBOARD_SENTINEL,
  keyboardEdit,
  NO_MODIFIERS,
  spendModifiers,
  tapModifier,
  type ModifierState,
} from "./screenPageModel";
import {
  parseScreenViewerMessage,
  type ScreenHostMessage,
  type ScreenKey,
  type ScreenModifier,
  type ScreenPointerMode,
  type ScreenViewerHostHandle,
  type ScreenViewerMessage,
} from "./screenViewerProtocol";

export interface RemoteModifier { key: ScreenModifier; label: string; name: string }
export const LINUX_MODIFIERS: RemoteModifier[] = [
  { key: "Control", label: "Ctrl", name: "Control" },
  { key: "Option", label: "Alt", name: "Alt" },
  { key: "Shift", label: "Shift", name: "Shift" },
];
export const MAC_MODIFIERS: RemoteModifier[] = [
  { key: "Command", label: "⌘", name: "Command" },
  { key: "Control", label: "⌃", name: "Control" },
  { key: "Option", label: "⌥", name: "Option" },
  { key: "Shift", label: "⇧", name: "Shift" },
];
const KEYS: { key: ScreenKey; label: string; name: string }[] = [
  { key: "Escape", label: "Esc", name: "Escape" },
  { key: "Tab", label: "Tab", name: "Tab" },
  { key: "Left", label: "←", name: "Left arrow" },
  { key: "Up", label: "↑", name: "Up arrow" },
  { key: "Down", label: "↓", name: "Down arrow" },
  { key: "Right", label: "→", name: "Right arrow" },
];

/**
 * Whether typing needs the system keyboard brought up: a phone or a tablet.
 * A computer's keyboard already reaches the view once it is clicked.
 */
export function needsSoftKeyboard(): boolean {
  if (Platform.OS !== "web") return true;
  try { return typeof window !== "undefined" && window.matchMedia("(pointer: coarse)").matches; } catch { return false; }
}

export interface RemoteViewHandle {
  /** The connection opened: start a session. */
  open(): void;
  /** The connection ended. */
  close(): void;
  /** Bytes from the remote, base64. */
  feed(chunk: string): void;
  fit(): void;
  /** Web: go full size. */
  fullSize(): void;
  /** Show or hide the system keyboard. */
  toggleKeyboard(): void;
}

export interface RemoteViewProps {
  backgroundColor: string;
  accessibilityLabel: string;
  /** Where the picture sits when the page is taller than it. */
  align: "top" | "center";
  /** Touch: tap clicks, drag scrolls. Trackpad: the finger moves a pointer. */
  mode?: ScreenPointerMode;
  /** Keys a phone keyboard lacks, in the slim bar while the keyboard is up. */
  modifiers: RemoteModifier[];
  /** Style of the whole view with its key bar, and of the picture's own size. */
  style?: StyleProp<ViewStyle>;
  viewStyle: StyleProp<ViewStyle>;
  /** RFB bytes the person's input produced, base64. */
  onSend(data: string): void;
  /** Everything else the viewer says (connected, stats, closed...). */
  onEvent(message: ScreenViewerMessage): void;
  onKeyboardChange?(open: boolean): void;
  /** Bottom padding under the key bar. */
  bottomInset?: number;
}

/**
 * The one view of a remote screen, whatever it shows: an agent's display, a
 * computer's own screen. The picture is the input surface. A mouse, wheel and
 * keyboard reach it directly; on a phone a tap clicks, a drag scrolls, a long
 * press right-clicks, two fingers zoom, and the system keyboard types live.
 * The only chrome is a slim bar of the keys a phone keyboard lacks, shown
 * while that keyboard is up.
 */
export const RemoteView = forwardRef<RemoteViewHandle, RemoteViewProps>(function RemoteView(
  { backgroundColor, accessibilityLabel, align, mode = "touch", modifiers, style, viewStyle, onSend, onEvent, onKeyboardChange, bottomInset = 0 },
  ref,
) {
  const { colors } = useAppTheme();
  const host = useRef<ScreenViewerHostHandle>(null);
  const input = useRef<TextInputInstance>(null);
  const viewerReady = useRef(false);
  const outbox = useRef<ScreenHostMessage[]>([]);
  const chunks = useRef<string[]>([]);
  const flushQueued = useRef(false);
  const [typing, setTyping] = useState(false);
  const [mods, setMods] = useState<ModifierState>(NO_MODIFIERS);
  const modsRef = useRef(mods);
  modsRef.current = mods;
  const settings = useRef({ align, mode });
  settings.current = { align, mode };
  const lastSettings = useRef("");

  // RFB bytes go to the viewer once per frame, so a burst of small socket
  // messages costs one bridge crossing.
  const flush = useCallback(() => {
    flushQueued.current = false;
    if (!viewerReady.current) return;
    const batch = outbox.current;
    outbox.current = [];
    if (chunks.current.length) {
      batch.push({ type: "rfb", data: chunks.current, at: Date.now() });
      chunks.current = [];
    }
    if (batch.length) host.current?.post(batch);
  }, []);
  const schedule = useCallback(() => {
    if (flushQueued.current) return;
    flushQueued.current = true;
    requestAnimationFrame(flush);
  }, [flush]);
  const toViewer = useCallback((message: ScreenHostMessage) => {
    // Bytes already queued belong before any later control message.
    if (chunks.current.length) {
      outbox.current.push({ type: "rfb", data: chunks.current, at: Date.now() });
      chunks.current = [];
    }
    outbox.current.push(message);
    schedule();
  }, [schedule]);

  // Mode and alignment follow their props, once the viewer can hear them.
  const applySettings = useCallback(() => {
    if (!viewerReady.current) return;
    const { align: a, mode: m } = settings.current;
    const key = `${a}:${m}`;
    if (key === lastSettings.current) return;
    lastSettings.current = key;
    host.current?.post([{ type: "mode", mode: m }, { type: "align", align: a }]);
  }, []);
  React.useEffect(applySettings, [align, mode, applySettings]);

  const setKeyboard = useCallback((open: boolean) => {
    setTyping(open);
    onKeyboardChange?.(open);
  }, [onKeyboardChange]);

  useImperativeHandle(ref, () => ({
    open() { toViewer({ type: "open" }); },
    close() {
      chunks.current = [];
      outbox.current = [];
      if (viewerReady.current) host.current?.post([{ type: "close" }]);
    },
    feed(chunk) {
      chunks.current.push(chunk);
      schedule();
    },
    fit() { toViewer({ type: "fit" }); },
    fullSize() { toViewer({ type: "fullsize" }); },
    toggleKeyboard() {
      if (typing) { input.current?.blur(); setKeyboard(false); }
      else { setKeyboard(true); requestAnimationFrame(() => input.current?.focus()); }
    },
  }), [schedule, setKeyboard, toViewer, typing]);

  const onHostMessage = useCallback((raw: string) => {
    const message = parseScreenViewerMessage(raw);
    if (!message) return;
    if (message.type === "ready") {
      viewerReady.current = true;
      lastSettings.current = "";
      applySettings();
      flush();
      return;
    }
    if (message.type === "send") { onSend(message.data); return; }
    onEvent(message);
  }, [applySettings, flush, onEvent, onSend]);

  const withMods = (extra: ScreenModifier[] = []) => [...new Set([...activeModifiers(modsRef.current), ...extra])];
  const sendKey = (key: ScreenKey) => {
    toViewer({ type: "key", key, mods: withMods() });
    setMods(spendModifiers(modsRef.current));
  };
  const onKeyboardText = (next: string) => {
    const edit = keyboardEdit(next);
    if (edit.kind === "text") {
      toViewer({ type: "text", text: edit.text, mods: withMods() });
      setMods(spendModifiers(modsRef.current));
    } else if (edit.kind === "backspace") {
      for (let i = 0; i < edit.count; i++) sendKey("BackSpace");
    }
  };
  const active = activeModifiers(mods);

  return (
    <View style={style}>
      <View style={[styles.view, { backgroundColor }, viewStyle]}>
        <ScreenViewerHost ref={host} onMessage={onHostMessage} backgroundColor={backgroundColor} accessibilityLabel={accessibilityLabel} />
      </View>
      {typing ? (
        <View style={[styles.bar, { borderTopColor: colors.borderSubtle, paddingBottom: Math.max(bottomInset, Spacing.xs) }]}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} keyboardShouldPersistTaps="always" contentContainerStyle={styles.keys}>
            {modifiers.map((m) => (
              <KeyButton key={m.key} label={m.label} name={m.name} active={active.includes(m.key)} locked={mods.locked.includes(m.key)}
                onPress={() => setMods(tapModifier(modsRef.current, m.key))} />
            ))}
            {KEYS.map((k) => <KeyButton key={k.key} label={k.label} name={k.name} onPress={() => sendKey(k.key)} />)}
          </ScrollView>
        </View>
      ) : null}
      {/* Off-screen field that only collects the system keyboard's keystrokes. */}
      <TextInput ref={input} value={KEYBOARD_SENTINEL} onChangeText={onKeyboardText}
        onSubmitEditing={() => sendKey("Return")} submitBehavior="submit"
        onKeyPress={(event) => {
          // Hardware keyboards on web report keys the field can't show.
          const named: Record<string, ScreenKey> = { ArrowLeft: "Left", ArrowRight: "Right", ArrowUp: "Up", ArrowDown: "Down", Escape: "Escape", Tab: "Tab" };
          const key = named[event.nativeEvent.key];
          if (key && Platform.OS === "web") { event.preventDefault?.(); sendKey(key); }
        }}
        onBlur={() => setKeyboard(false)}
        autoCorrect={false} autoCapitalize="none" autoComplete="off" spellCheck={false}
        keyboardType={Platform.OS === "android" ? "visible-password" : "default"}
        accessibilityLabel="Type on the remote screen" style={styles.hiddenInput} caretHidden />
    </View>
  );
});

function KeyButton({ label, name, active, locked, onPress }: {
  label: string;
  name: string;
  active?: boolean;
  locked?: boolean;
  onPress(): void;
}) {
  const { colors } = useAppTheme();
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={name} accessibilityState={{ selected: !!active }} onPress={onPress} hitSlop={4}
      style={({ pressed }) => [styles.key, {
        backgroundColor: active ? colors.accentSoft : pressed ? colors.surfaceSubtle : colors.bgSurface,
        borderColor: locked ? colors.accent : colors.borderSubtle,
        borderWidth: locked ? 2 : StyleSheet.hairlineWidth,
      }]}>
      <AppText variant="label" style={{ color: active ? colors.accentStrong : colors.textPrimary }}>{label}</AppText>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  view: { overflow: "hidden" },
  bar: { borderTopWidth: StyleSheet.hairlineWidth, paddingTop: Spacing.xs },
  keys: { gap: Spacing.xs, paddingHorizontal: Spacing.sm, alignItems: "center" },
  key: { minWidth: TouchTarget, height: TouchTarget, paddingHorizontal: Spacing.sm, alignItems: "center", justifyContent: "center", borderRadius: Radii.sm, ...ContinuousCorners },
  hiddenInput: { position: "absolute", width: 1, height: 1, opacity: 0, left: -100, bottom: 0 },
});
