import React, { useEffect, useRef, useState } from "react";
import { AppState, View, type StyleProp, type ViewStyle } from "react-native";
import { Image } from "expo-image";
import { useReducedMotion } from "react-native-reanimated";
import { useThemeContext } from "../../theme";
import type { BrainCatState } from "../mewla/brainCatState";
import { findPetPack, petTransition, type PetClipName, type PetPack } from "./petModel";
import { DEFAULT_PET_ID, PET_PACKS } from "./petPacks";

interface PetSpriteProps {
  state: BrainCatState;
  /** Width and height in points; every clip shares one square canvas. */
  size: number;
  /** Off when the screen is hidden; reduced motion and background force it off. */
  animate?: boolean;
  /** Another pet than the chosen one, for the picker's preview. */
  petId?: string;
  style?: StyleProp<ViewStyle>;
}

/**
 * Brain as the chosen pet, playing its state's clip. Moving between the seal
 * and its feet plays the hop out or back in first. Without motion it holds
 * the clip's first frame (native only; browsers can't pause an image).
 */
export function PetSprite({ state, size, animate = true, petId, style }: PetSpriteProps) {
  const chosen = useThemeContext().petId;
  const pack = findPetPack(PET_PACKS, petId ?? chosen, DEFAULT_PET_ID);
  const moving = usePetMotion(animate);
  const clip = pack.clips[usePetClip(pack, state, moving)];
  return (
    <View
      style={[{ width: size, height: size, pointerEvents: "none" }, style]}
      accessible={false}
      importantForAccessibility="no-hide-descendants"
    >
      <Image
        source={clip.source}
        autoplay={moving}
        contentFit="contain"
        style={{ width: size, height: size }}
      />
    </View>
  );
}

/** The clip on screen: the state's own, after any hop has played through once. */
function usePetClip(pack: PetPack, state: BrainCatState, moving: boolean): PetClipName {
  const [shown, setShown] = useState<PetClipName>(state);
  const settled = useRef(state);
  useEffect(() => {
    const from = settled.current;
    settled.current = state;
    const hop = moving ? petTransition(from, state) : null;
    if (!hop) {
      setShown(state);
      return;
    }
    setShown(hop);
    const timer = setTimeout(() => setShown(state), pack.clips[hop].durationMs);
    return () => clearTimeout(timer);
  }, [moving, pack, state]);
  return shown;
}

/** Motion runs only while wanted, allowed by the OS, and the app is in front. */
function usePetMotion(animate: boolean): boolean {
  const reduced = useReducedMotion();
  const [active, setActive] = useState(AppState.currentState !== "background");
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (next) => setActive(next === "active"));
    return () => subscription.remove();
  }, []);
  return animate && !reduced && active;
}
