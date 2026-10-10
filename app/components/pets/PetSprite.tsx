import React, { useEffect, useRef, useState } from "react";
import { AppState, Platform, View, type ImageSourcePropType, type StyleProp, type ViewStyle } from "react-native";
import { Asset } from "expo-asset";
import { Image } from "expo-image";
import { useReducedMotion } from "react-native-reanimated";
import { useThemeContext } from "../../theme";
import type { BrainCatState } from "../mewla/brainCatState";
import { findPetPack, petInSeal, petTransition, type PetClipName, type PetPack } from "./petModel";
import { DEFAULT_PET_ID, PET_PACKS } from "./petPacks";

interface PetSpriteProps {
  state: BrainCatState;
  /** Width and height in points; every clip shares one square canvas. */
  size: number;
  /** Off when the screen is hidden; reduced motion and background force it off. */
  animate?: boolean;
  /** Another pet than the chosen one, for the picker's preview. */
  petId?: string;
  /**
   * Counts happy hops asked for by the user. Each new count plays the jumping
   * clip once on a pet standing on its feet; the seal ignores it.
   */
  cheer?: number;
  /**
   * Without motion, draw the pack's portrait (a whole still drawing) instead
   * of holding the clip, which a browser can't pause.
   */
  stillPortrait?: boolean;
  style?: StyleProp<ViewStyle>;
}

/**
 * Brain as the chosen pet, playing its state's clip. Moving between the seal
 * and its feet plays the hop out or back in first. Without motion it holds
 * the clip's first frame (native only; browsers can't pause an image).
 */
export function PetSprite({ state, size, animate = true, petId, cheer = 0, stillPortrait = false, style }: PetSpriteProps) {
  const chosen = useThemeContext().petId;
  const pack = findPetPack(PET_PACKS, petId ?? chosen, DEFAULT_PET_ID);
  const moving = usePetMotion(animate);
  const shown = usePetClip(pack, state, moving, cheer);
  const clip = pack.clips[shown.name];
  // A clip with bleed is drawn larger around the same box, so the pet keeps
  // its size and seat while the hop's ring overflows the box.
  const bleed = (clip.bleed ?? 0) * size;
  if (stillPortrait && !moving) {
    return (
      <View style={[{ width: size, height: size, pointerEvents: "none" }, style]} accessible={false} importantForAccessibility="no-hide-descendants">
        <Image source={pack.portrait} contentFit="contain" style={{ width: size, height: size }} />
      </View>
    );
  }
  return (
    <View
      style={[{ width: size, height: size, overflow: "visible", pointerEvents: "none" }, style]}
      accessible={false}
      importantForAccessibility="no-hide-descendants"
    >
      <Image
        source={clip.loop ? clip.source : freshPlay(clip.source, shown.play)}
        autoplay={moving}
        contentFit="contain"
        style={
          bleed
            ? { position: "absolute", left: -bleed, top: -bleed, width: size + 2 * bleed, height: size + 2 * bleed }
            : { width: size, height: size }
        }
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

/**
 * The clip on screen, after any hop has played through once; play counts
 * every change. A cheer plays the jumping clip once, then the state's clip.
 */
function usePetClip(
  pack: PetPack,
  state: BrainCatState,
  moving: boolean,
  cheer: number,
): { name: PetClipName; play: number } {
  const [shown, setShown] = useState<{ name: PetClipName; play: number }>({ name: state, play: 0 });
  const settled = useRef(state);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cheered = useRef(cheer);
  useEffect(() => {
    const show = (name: PetClipName) => setShown((current) => ({ name, play: current.play + 1 }));
    const from = settled.current;
    settled.current = state;
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    const hop = moving ? petTransition(from, state) : null;
    if (!hop) {
      show(state);
      return;
    }
    show(hop);
    timer.current = setTimeout(() => {
      timer.current = null;
      show(state);
    }, pack.clips[hop].durationMs);
  }, [moving, pack, state]);
  useEffect(() => {
    if (cheer === cheered.current) return;
    cheered.current = cheer;
    // A hop or a cheer still playing keeps the pet busy; cheers never stack.
    if (!moving || timer.current || petInSeal(state)) return;
    setShown((current) => ({ name: "delivered", play: current.play + 1 }));
    timer.current = setTimeout(() => {
      timer.current = null;
      setShown((current) => ({ name: settled.current, play: current.play + 1 }));
    }, pack.clips.delivered.durationMs);
  }, [cheer, moving, pack, state]);
  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);
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
