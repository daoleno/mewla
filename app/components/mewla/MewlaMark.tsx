import React from "react";
import { Image } from "expo-image";

const MARK = require("../../assets/branding/mewla-mark.webp");

/**
 * The Mewla mark: the default pet's face in the vermilion seal, as on the
 * app icon and the landing's header. It is the logo, not Brain, so it never
 * moves or follows the chosen pet.
 */
export function MewlaMark({ size }: { size: number }) {
  return <Image source={MARK} accessible={false} contentFit="contain" style={{ width: size, height: size }} />;
}
