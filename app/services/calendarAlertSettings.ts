import AsyncStorage from "@react-native-async-storage/async-storage";
import { getMewlaLiveModule } from "../modules/mewla-live/src";

const storageKey = "mewla.calendar.strong_alerts.v1";
let strong = true;
const listeners = new Set<(value: boolean) => void>();

/** "Strong calendar alerts": on unless the person turned it off. Read once at launch. */
export async function loadStrongCalendarAlerts(): Promise<boolean> {
  try {
    strong = (await AsyncStorage.getItem(storageKey)) !== "off";
  } catch {
    strong = true;
  }
  getMewlaLiveModule()?.calendarAlertsSetStrong?.(strong);
  return strong;
}

export function strongCalendarAlerts(): boolean {
  return strong;
}

export async function setStrongCalendarAlerts(value: boolean) {
  strong = value;
  getMewlaLiveModule()?.calendarAlertsSetStrong?.(value);
  for (const listener of listeners) listener(value);
  await AsyncStorage.setItem(storageKey, value ? "on" : "off");
}

export function onStrongCalendarAlertsChange(listener: (value: boolean) => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
