import { useEffect, useState } from "react";
import AsyncStorage from "@react-native-async-storage/async-storage";

// Labs → Virtual screen. Off until the person turns it on; it only decides
// whether Screen shows a Linux computer's agent browsers, so a per-device
// pointer is enough. Nothing is stored on the computer.
const KEY = "mewla.labs.virtualScreen";
let value = false;
let loaded = false;
const listeners = new Set<(on: boolean) => void>();

async function load() {
  if (loaded) return;
  loaded = true;
  try { value = (await AsyncStorage.getItem(KEY)) === "1"; } catch { value = false; }
  listeners.forEach((listen) => listen(value));
}

export async function setVirtualScreenLabs(on: boolean) {
  value = on;
  listeners.forEach((listen) => listen(on));
  try { await AsyncStorage.setItem(KEY, on ? "1" : "0"); } catch { /* the switch still holds until the app closes */ }
}

export function useVirtualScreenLabs(): { enabled: boolean; set(on: boolean): Promise<void> } {
  const [enabled, setEnabled] = useState(value);
  useEffect(() => {
    listeners.add(setEnabled);
    void load();
    setEnabled(value);
    return () => { listeners.delete(setEnabled); };
  }, []);
  return { enabled, set: setVirtualScreenLabs };
}
