import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { PermissionsAndroid, Platform } from "react-native";
import Constants from "expo-constants";
import { MewlaPhone, parsePhoneStatus, type PhoneStatus } from "../modules/mewla-phone/src";
import { resolveDistributionChannel } from "../services/appUpdate";
import { changePhone } from "../services/phoneApi";
import { phoneLinkHost } from "../services/phoneLinkProcess";
import type { PhoneLinkState } from "../services/phoneLink";
import { useCurrentServer } from "./currentServer";

export type PhoneUseLink = PhoneLinkState | "off";

interface PhoneUseValue {
  /** Android, a build that carries phone use, and an Android that can do it. */
  available: boolean;
  enabled: boolean;
  status: PhoneStatus | null;
  /** The signed connection to the current computer. */
  link: PhoneUseLink;
  /** Whether everything the agent needs is in place and the computer can use the phone. */
  ready: boolean;
  error: string | null;
  turnOn(): Promise<void>;
  turnOff(): Promise<void>;
  /** Poll the live checks while a screen shows them; returns the release. */
  watchSetup(): () => void;
  pair(): Promise<boolean>;
  takeOver(): void;
  giveBack(): void;
  stop(): void;
  handoffDone(): void;
}

const noop = () => undefined;
const Unavailable: PhoneUseValue = {
  available: false,
  enabled: false,
  status: null,
  link: "off",
  ready: false,
  error: null,
  turnOn: async () => undefined,
  turnOff: async () => undefined,
  watchSetup: () => noop,
  pair: async () => false,
  takeOver: noop,
  giveBack: noop,
  stop: noop,
  handoffDone: noop,
};

const PhoneUseContext = createContext<PhoneUseValue>(Unavailable);

/** Whether this build may offer phone use: the GitHub APK on Android, never the Play Store build. */
export function phoneUseAvailable(): boolean {
  if (Platform.OS !== "android" || !MewlaPhone) return false;
  if (resolveDistributionChannel("android", Constants.expoConfig?.extra) === "play-store") return false;
  const status = parsePhoneStatus(MewlaPhone.status());
  return status?.supported === true;
}

/**
 * The Phone use switch in Labs and what its screens read. The one current computer is the only
 * place agents come from: the signed link follows it, and a server switch closes the old link and
 * opens one to the new computer.
 */
export function PhoneUseProvider({ children }: { children: ReactNode }) {
  const available = useMemo(() => phoneUseAvailable(), []);
  const { currentServer, hydrated } = useCurrentServer();
  const [status, setStatus] = useState<PhoneStatus | null>(() => (available && MewlaPhone ? parsePhoneStatus(MewlaPhone.status()) : null));
  const [link, setLink] = useState<PhoneUseLink>("off");
  const [error, setError] = useState<string | null>(null);
  const watchers = useRef(0);

  useEffect(() => {
    if (!available || !MewlaPhone) return;
    const subscription = MewlaPhone.addListener("onStatus", (event) => {
      const next = parsePhoneStatus(event.json);
      if (next) setStatus(next);
    });
    return () => subscription.remove();
  }, [available]);

  // The checks change when the person comes back from Settings; poll fast only while they are on screen.
  useEffect(() => {
    if (!available || !MewlaPhone) return;
    const timer = setInterval(() => {
      const next = parsePhoneStatus(MewlaPhone!.status());
      if (next) setStatus(next);
    }, watchers.current > 0 ? 1_000 : 10_000);
    return () => clearInterval(timer);
  }, [available, status?.enabled]);

  const enabled = status?.enabled === true;
  const serverId = currentServer?.id ?? null;
  // The link belongs to the process (see phoneLinkHost): this page watches it and decides which computer it is for.
  useEffect(() => phoneLinkHost.watch((state) => setLink(state)), []);
  useEffect(() => {
    if (!available || !MewlaPhone || !enabled) {
      phoneLinkHost.release();
      return;
    }
    // Until the saved servers are read there is nothing to decide, and a link brought up without a screen must not flap.
    if (!hydrated) return;
    if (!currentServer) {
      phoneLinkHost.release();
      return;
    }
    const server = currentServer;
    // The computer's own switch for its Workers, then the connection.
    void changePhone(server, { enabled: true }).catch((cause: unknown) => setError(cause instanceof Error ? cause.message : "Not reachable"));
    phoneLinkHost.hold({ server, native: MewlaPhone, appVersion: String(Constants.expoConfig?.version ?? "") });
    // The link belongs to the computer, not to its object identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [available, enabled, hydrated, serverId]);

  const turnOn = useCallback(async () => {
    if (!MewlaPhone) return;
    setError(null);
    if (Platform.OS === "android" && Number(Platform.Version) >= 33) {
      await PermissionsAndroid.request(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS).catch(() => undefined);
    }
    MewlaPhone.setEnabled(true);
    setStatus(parsePhoneStatus(MewlaPhone.status()));
  }, []);

  const turnOff = useCallback(async () => {
    if (!MewlaPhone) return;
    MewlaPhone.setEnabled(false);
    setStatus(parsePhoneStatus(MewlaPhone.status()));
    if (currentServer) await changePhone(currentServer, { enabled: false }).catch(() => undefined);
  }, [currentServer]);

  const watchSetup = useCallback(() => {
    watchers.current += 1;
    if (MewlaPhone) {
      const next = parsePhoneStatus(MewlaPhone.status());
      if (next) setStatus(next);
    }
    return () => {
      watchers.current = Math.max(0, watchers.current - 1);
    };
  }, []);

  const value = useMemo<PhoneUseValue>(() => {
    if (!available || !MewlaPhone) return Unavailable;
    const native = MewlaPhone;
    return {
      available,
      enabled,
      status,
      link,
      ready: enabled && status?.paired === true && status?.helper === true && link === "online",
      error,
      turnOn,
      turnOff,
      watchSetup,
      pair: () => native.pair(null),
      takeOver: () => native.takeOver(),
      giveBack: () => native.giveBack(),
      stop: () => native.stop(),
      handoffDone: () => native.handoffDone(),
    };
  }, [available, enabled, status, link, error, turnOn, turnOff, watchSetup]);

  return <PhoneUseContext.Provider value={value}>{children}</PhoneUseContext.Provider>;
}

export function usePhoneUse(): PhoneUseValue {
  return useContext(PhoneUseContext);
}
