import * as SecureStore from "expo-secure-store";
import AsyncStorage from "@react-native-async-storage/async-storage";
import { Platform } from "react-native";

// expo-secure-store has no web implementation. On web these values live in
// AsyncStorage (localStorage) under this prefix; existing keys depend on it.
const WEB_SECURE_STORE_PREFIX = "mewla:secure:";

/** Device-secret storage: the keychain/keystore on mobile, prefixed AsyncStorage on web. */
export const secureStorage = {
  async getItemAsync(key: string): Promise<string | null> {
    if (Platform.OS === "web") {
      return AsyncStorage.getItem(`${WEB_SECURE_STORE_PREFIX}${key}`);
    }
    return SecureStore.getItemAsync(key);
  },
  async setItemAsync(key: string, value: string): Promise<void> {
    if (Platform.OS === "web") {
      await AsyncStorage.setItem(`${WEB_SECURE_STORE_PREFIX}${key}`, value);
      return;
    }
    await SecureStore.setItemAsync(key, value);
  },
  async deleteItemAsync(key: string): Promise<void> {
    if (Platform.OS === "web") {
      await AsyncStorage.removeItem(`${WEB_SECURE_STORE_PREFIX}${key}`);
      return;
    }
    await SecureStore.deleteItemAsync(key);
  },
};
