import React, { useEffect, useState } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { Image } from "expo-image";
import * as Haptics from "expo-haptics";
import { ContinuousCorners, Radii, useAppTheme } from "../../constants/tokens";
import { useThemeContext } from "../../theme";
import { AppText } from "../ui/AppText";
import type { BrainCatState } from "../mewla/brainCatState";
import { PetSprite } from "../pets/PetSprite";
import { findPetPack } from "../pets/petModel";
import { DEFAULT_PET_ID, PET_PACKS } from "../pets/petPacks";

/** The preview runs through a few of Brain's states so the pet shows its range. */
const PREVIEW_STATES: readonly BrainCatState[] = ["delegating", "working", "delivered", "idle"];
const PREVIEW_STEP_MS = 3200;
const PREVIEW_SIZE = 112;
const PORTRAIT_SIZE = 52;

/** Settings → Pet: the chosen pet acting out Brain's states, and every pet to pick from. */
export function PetPicker({ animate = true }: { animate?: boolean }) {
  const { colors, theme } = useAppTheme();
  const { petId, setPetId } = useThemeContext();
  const pet = findPetPack(PET_PACKS, petId, DEFAULT_PET_ID);
  const [step, setStep] = useState(0);
  useEffect(() => {
    if (!animate) return;
    const timer = setInterval(() => setStep((current) => current + 1), PREVIEW_STEP_MS);
    return () => clearInterval(timer);
  }, [animate, petId]);
  const choose = (id: string) => {
    if (id === petId) return;
    void Haptics.selectionAsync();
    setStep(0);
    void setPetId(id);
  };
  const border = theme.isLight ? colors.borderSubtle : theme.materials.stroke;
  return (
    <View style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: border }]}>
      <View style={styles.preview}>
        <PetSprite
          state={PREVIEW_STATES[step % PREVIEW_STATES.length]}
          size={PREVIEW_SIZE}
          animate={animate}
        />
        <AppText variant="heading">{pet.name.en}</AppText>
        <AppText variant="caption" tone="tertiary">
          Shows what Brain is doing.
        </AppText>
      </View>
      <View style={styles.grid} accessibilityRole="radiogroup" accessibilityLabel="Pet">
        {PET_PACKS.map((pack) => {
          const selected = pack.id === pet.id;
          return (
            <Pressable
              key={pack.id}
              onPress={() => choose(pack.id)}
              accessibilityRole="radio"
              accessibilityState={{ checked: selected }}
              accessibilityLabel={pack.name.en}
              style={({ pressed }) => [
                styles.tile,
                {
                  borderColor: selected ? colors.textPrimary : "transparent",
                  backgroundColor: selected ? colors.bgPrimary : "transparent",
                },
                pressed ? styles.pressed : null,
              ]}
            >
              <Image source={pack.portrait} style={styles.portrait} contentFit="contain" />
              <AppText
                variant="micro"
                tone={selected ? "primary" : "secondary"}
                numberOfLines={1}
              >
                {pack.name.en}
              </AppText>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    borderRadius: Radii.card,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    paddingVertical: 16,
    paddingHorizontal: 10,
    marginBottom: 26,
    gap: 12,
  },
  preview: {
    alignItems: "center",
    gap: 2,
  },
  grid: {
    flexDirection: "row",
    flexWrap: "wrap",
    justifyContent: "center",
  },
  tile: {
    width: "20%",
    minWidth: 60,
    alignItems: "center",
    gap: 2,
    paddingVertical: 6,
    borderRadius: Radii.sm,
    borderWidth: 1.5,
  },
  pressed: {
    opacity: 0.7,
  },
  portrait: {
    width: PORTRAIT_SIZE,
    height: PORTRAIT_SIZE,
  },
});
