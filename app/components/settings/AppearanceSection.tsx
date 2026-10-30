import React from "react";
import { Image } from "expo-image";
import { useRouter } from "expo-router";
import { StyleSheet } from "react-native";
import { ListRow, ListSection } from "../ui";
import { useThemeContext } from "../../theme";
import { findPetPack } from "../pets/petModel";
import { DEFAULT_PET_ID, PET_PACKS } from "../pets/petPacks";

const THEME_CHOICES = [
  { label: "Auto", value: "system", icon: "contrast" },
  { label: "Light", value: "classic-light", icon: "sun" },
  { label: "Dark", value: "classic-dark", icon: "moon" },
] as const;

/** Settings → Appearance: the theme, one row per choice. */
export function AppearanceSection() {
  const { preference, setPreference } = useThemeContext();
  const current = preference === "classic-light" || preference === "classic-dark" ? preference : "system";
  return (
    <ListSection>
      {THEME_CHOICES.map((choice) => (
        <ListRow
          key={choice.value}
          title={choice.label}
          icon={choice.icon}
          accessory="check"
          selected={choice.value === current}
          onPress={() => void setPreference(choice.value)}
        />
      ))}
    </ListSection>
  );
}

/** Settings → Pet: the chosen pet, opening the page that picks one. */
export function PetSection() {
  const router = useRouter();
  const { petId } = useThemeContext();
  const pet = findPetPack(PET_PACKS, petId, DEFAULT_PET_ID);
  return (
    <ListSection>
      <ListRow
        title="Pet"
        value={pet.name.en}
        leading={<Image source={pet.portrait} style={styles.portrait} contentFit="contain" />}
        accessory="chevron"
        onPress={() => router.push("/settings/pet")}
      />
    </ListSection>
  );
}

const styles = StyleSheet.create({
  portrait: { width: 30, height: 30 },
});
