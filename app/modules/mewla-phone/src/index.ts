import { requireOptionalNativeModule, requireNativeViewManager } from "expo-modules-core";
import type { ComponentType } from "react";
import type { ViewProps } from "react-native";

/** What the helper and the phone's settings say, as the setup screen reads it live. */
export interface PhoneStatus {
  supported: boolean;
  sdk: number;
  enabled: boolean;
  /** The App's connection to the computer is up. */
  link: boolean;
  developer_options: boolean;
  wireless_debugging: boolean;
  wifi: boolean;
  paired: boolean;
  helper: boolean;
  accessibility: boolean;
  notifications: boolean;
  /** The word the daemon is told: ready, setup, person_in_control or stopped. */
  state: "ready" | "setup" | "person_in_control" | "stopped";
  /** The agent's display size; 0 until the display exists. */
  w: number;
  h: number;
  /** The agent working now, or null. */
  agent: string | null;
  /** What the agent asked the person to do, or null. */
  handoff: string | null;
}

export interface PhoneNativeModule {
  status(): string;
  setEnabled(enabled: boolean): void;
  /** Pairs with this phone's own Wireless debugging; `null` reads the code off the screen. */
  pair(code: string | null): Promise<boolean>;
  ensureHelper(): Promise<boolean>;
  /** One request frame from the daemon; resolves with the result frame. */
  execute(requestJson: string): Promise<string>;
  /** Tells the notification whether the connection to the computer is up. */
  setLink?(online: boolean): void;
  takeOver(): void;
  giveBack(): void;
  stop(): void;
  handoffDone(): void;
  openWirelessDebugging(): void;
  openDeveloperSettings(): void;
  openAccessibilitySettings(): void;
  openAppSettings(): void;
  showPairingCodeBox(): void;
  addListener(event: "onStatus" | "onEvent", listener: (event: { json: string }) => void): { remove(): void };
}

/** Absent on iOS and in a binary built before phone use: the feature is then simply not there. */
export const MewlaPhone: PhoneNativeModule | null = requireOptionalNativeModule<PhoneNativeModule>("MewlaPhone");

export interface PhoneLiveViewProps extends ViewProps {
  /** Touches drive the agent's screen (Take over) instead of being ignored (Watch). */
  interactive?: boolean;
}

export const PhoneLiveView: ComponentType<PhoneLiveViewProps> | null = MewlaPhone
  ? requireNativeViewManager<PhoneLiveViewProps>("MewlaPhone")
  : null;

export function parsePhoneStatus(json: string): PhoneStatus | null {
  try {
    return JSON.parse(json) as PhoneStatus;
  } catch {
    return null;
  }
}
