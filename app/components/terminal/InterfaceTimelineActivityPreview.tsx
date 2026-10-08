import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { imageReference } from "../../services/imageSource";
import { AppImage } from "./AppImage";

export function ActivityPreview({ path, chrome }: { path: string; failed?: boolean; chrome: TerminalThemeChrome }) {
  return <AppImage source={imageReference(path, path.split("/").pop() || "Tool image")} chrome={chrome} />;
}
