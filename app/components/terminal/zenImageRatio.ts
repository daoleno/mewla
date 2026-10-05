type ImageLoadEvent = {
  /** iOS/Android: the decoded image size. */
  source?: { width?: number; height?: number };
  /** Web: react-native-web forwards the DOM load event; its target is the <img>. */
  target?: { naturalWidth?: number; naturalHeight?: number };
};

/** Aspect ratio of a loaded image on any platform; 1 when the size is unknown. */
export function loadedImageRatio(nativeEvent: unknown): number {
  const event = (nativeEvent ?? {}) as ImageLoadEvent;
  const width = event.source?.width ?? event.target?.naturalWidth ?? 0;
  const height = event.source?.height ?? event.target?.naturalHeight ?? 0;
  return width > 0 && height > 0 ? width / height : 1;
}
