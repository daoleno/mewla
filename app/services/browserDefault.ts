import AsyncStorage from "@react-native-async-storage/async-storage";

// The browser the user last opened, per server. Only a pointer; profiles and
// sign-ins stay on that server.
const key = (serverId: string) => `zen.browser.default.${serverId}`;

export async function getDefaultBrowserId(serverId: string): Promise<string | null> {
  try { return (await AsyncStorage.getItem(key(serverId))) || null; } catch { return null; }
}

export async function setDefaultBrowserId(serverId: string, id: string): Promise<void> {
  try { await AsyncStorage.setItem(key(serverId), id); } catch { /* A lost pointer only changes which browser opens first. */ }
}
