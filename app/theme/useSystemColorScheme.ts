import { useColorScheme, type ColorSchemeName } from "react-native";

/** The OS appearance; follows changes while the app runs. */
export function useSystemColorScheme(): ColorSchemeName | null {
  return useColorScheme();
}
