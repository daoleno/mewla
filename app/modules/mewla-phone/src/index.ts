import { requireOptionalNativeModule, requireNativeViewManager } from "expo-modules-core";
import type { ComponentType } from "react";
import type { ViewProps } from "react-native";

/** Where the guided setup is: the step, what only the person can do, and whether it has stopped getting anywhere. */
export interface PhoneGuide {
  step: "developer" | "wireless" | "pair" | null;
  /** "allow": the system's prompt for this network; "lock": the screen lock. */
  needs: "allow" | "lock" | null;
  stalled: boolean;
}

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
  /** Why the helper is not up (what its last start attempt ran into), or null. */
  problem: string | null;
  accessibility: boolean;
  /** Android may block switching the accessibility service on until "Allow restricted settings" is chosen in App info (Android 13+, not installed from the Play Store). */
  restricted: boolean;
  /** The App may write secure settings (granted once through its helper): it restores Wireless debugging and its own accessibility service itself. */
  self_healing: boolean;
  /** The guided setup while it walks Settings, else null. */
  guide: PhoneGuide | null;
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
  /** The agent's display has shown a picture since it was made; until then there is nothing to show. */
  drawn: boolean;
  /** The app being opened on the agent's display now, or null. */
  opening: string | null;
  /** The app the last launch could not open, or null. */
  launch_failed: string | null;
}

export interface PhoneNativeModule {
  status(): string;
  setEnabled(enabled: boolean): void;
  /** Pairs with this phone's own Wireless debugging; `null` reads the code off the screen. */
  pair(code: string | null): Promise<boolean>;
  ensureHelper(): Promise<boolean>;
  /** Switches the accessibility service on by itself when it may; false when the person must do it in Settings. */
  enableAccessibility(): boolean;
  /** One request frame from the daemon; resolves with the result frame. */
  execute(requestJson: string): Promise<string>;
  /** Tells the notification whether the connection to the computer is up. */
  setLink?(online: boolean): void;
  takeOver(): void;
  giveBack(): void;
  stop(): void;
  handoffDone(): void;
  /** Floats the agent's screen in a small window; false when the phone cannot. */
  enterPip?(w: number, h: number): boolean;
  /** Leaving the App floats the screen by itself while this is on. */
  setPipAuto?(on: boolean, w: number, h: number): void;
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
  /** Frames per second this view needs (default 12); the shared stream runs at the highest mounted view. */
  fps?: number;
  /** Frame width in pixels this view needs (default 540). */
  maxWidth?: number;
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
