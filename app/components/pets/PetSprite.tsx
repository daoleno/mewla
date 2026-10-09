import React, { useEffect, useRef, useState } from "react";
import { AppState, Platform, View, type ImageSourcePropType, type StyleProp, type ViewStyle } from "react-native";
import { Asset } from "expo-asset";
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
  const shown = usePetClip(pack, state, moving);
  const clip = pack.clips[shown.name];
  return (
    <View
      style={[{ width: size, height: size, pointerEvents: "none" }, style]}
      accessible={false}
      importantForAccessibility="no-hide-descendants"
    >
      <Image
        source={clip.loop ? clip.source : freshPlay(clip.source, shown.play)}
        autoplay={moving}
        contentFit="contain"
        style={{ width: size, height: size }}
      />
    </View>
  );
}

/**
 * A browser keeps one animation per image URL, so a one-shot shown again would
 * hold its last frame. On web each play of a one-shot gets its own URL.
 */
function freshPlay(source: ImageSourcePropType, play: number): ImageSourcePropType {
  if (Platform.OS !== "web") return source;
  return { uri: `${Asset.fromModule(source as number).uri}#${play}` };
}

/** The clip on screen, after any hop has played through once; play counts every change. */
function usePetClip(pack: PetPack, state: BrainCatState, moving: boolean): { name: PetClipName; play: number } {
  const [shown, setShown] = useState<{ name: PetClipName; play: number }>({ name: state, play: 0 });
  const settled = useRef(state);
  useEffect(() => {
    const show = (name: PetClipName) => setShown((current) => ({ name, play: current.play + 1 }));
    const from = settled.current;
    settled.current = state;
    const hop = moving ? petTransition(from, state) : null;
    if (!hop) {
      show(state);
      return;
    }
    show(hop);
    const timer = setTimeout(() => show(state), pack.clips[hop].durationMs);
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
