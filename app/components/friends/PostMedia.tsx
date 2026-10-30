import React, { useEffect, useRef, useState } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { Image } from "expo-image";
import { useEvent } from "expo";
import { VideoView, useVideoPlayer, type VideoPlayer } from "expo-video";
import * as Haptics from "expo-haptics";
import { Radii, useAppTheme } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { Icon } from "../icons/Icon";
import { durationLabel, mediaLayout, singleMediaAspect, type FriendsMedia } from "../../services/friendsApi";

const GAP = 3;

interface PostMediaProps {
  media: FriendsMedia[];
  /** Whether this post's video is the one allowed to play right now. */
  active: boolean;
  /** Called when this post's video starts, so the list lets only one play. */
  onPlay(): void;
  onStop(): void;
}

/**
 * The pictures or video of a post, in one frame. Pictures are lazy (expo-image
 * loads them as the row mounts) and sit in a grid of 1, 2, 3 or 4. A video
 * shows its poster and length; nothing is downloaded or decoded until the
 * first tap, which plays it muted. A second tap goes full screen.
 */
export const PostMedia = React.memo(function PostMedia({ media, active, onPlay, onStop }: PostMediaProps) {
  const { colors } = useAppTheme();
  if (media.length === 0) return null;
  const first = media[0];
  const frame = { borderColor: colors.borderSubtle, backgroundColor: colors.surfaceSubtle };
  if (first.kind === "video") {
    return (
      <View style={[styles.frame, frame, { aspectRatio: singleMediaAspect(first.width, first.height) }]}>
        <PostVideo media={first} active={active} onPlay={onPlay} onStop={onStop} />
      </View>
    );
  }
  const shown = media.slice(0, 4);
  const layout = mediaLayout(shown.length);
  const tile = (item: FriendsMedia, index: number, style?: object) => (
    <Image
      key={`${item.url}-${index}`}
      source={{ uri: item.url }}
      style={[styles.tile, style]}
      contentFit="cover"
      transition={120}
      recyclingKey={item.url}
      accessibilityIgnoresInvertColors
      accessibilityLabel={`Picture ${index + 1} of ${shown.length}`}
    />
  );
  if (layout === "single") {
    return (
      <View style={[styles.frame, frame, { aspectRatio: singleMediaAspect(first.width, first.height) }]}>
        {tile(first, 0, StyleSheet.absoluteFill as object)}
      </View>
    );
  }
  return (
    <View style={[styles.frame, frame, { aspectRatio: layout === "pair" ? 2 : 1.25 }]}>
      {layout === "pair" ? (
        <View style={styles.row}>
          {tile(shown[0], 0, styles.half)}
          {tile(shown[1], 1, styles.half)}
        </View>
      ) : layout === "trio" ? (
        <View style={styles.row}>
          {tile(shown[0], 0, styles.half)}
          <View style={[styles.half, styles.column]}>
            {tile(shown[1], 1, styles.half)}
            {tile(shown[2], 2, styles.half)}
          </View>
        </View>
      ) : (
        <View style={styles.column}>
          <View style={styles.row}>
            {tile(shown[0], 0, styles.half)}
            {tile(shown[1], 1, styles.half)}
          </View>
          <View style={styles.row}>
            {tile(shown[2], 2, styles.half)}
            {tile(shown[3], 3, styles.half)}
          </View>
        </View>
      )}
    </View>
  );
});

type VideoStage = "poster" | "playing";

function PostVideo({ media, active, onPlay, onStop }: { media: FriendsMedia; active: boolean; onPlay(): void; onStop(): void }) {
  const [stage, setStage] = useState<VideoStage>("poster");
  const [muted, setMuted] = useState(true);
  const view = useRef<VideoView>(null);

  // Another post's video took over, or the row was told to stop.
  useEffect(() => {
    if (!active && stage === "playing") setStage("poster");
  }, [active, stage]);

  const start = () => {
    void Haptics.selectionAsync();
    setMuted(true);
    setStage("playing");
    onPlay();
  };

  if (stage === "poster") {
    return (
      <Pressable style={StyleSheet.absoluteFill} accessibilityRole="button" accessibilityLabel={`Play video, ${durationLabel(media.duration_ms ?? 0)}`} onPress={start}>
        {media.poster ? (
          <Image source={{ uri: media.poster.url }} style={StyleSheet.absoluteFill} contentFit="cover" transition={120} recyclingKey={media.poster.url} accessibilityIgnoresInvertColors />
        ) : null}
        <View style={styles.centerBadge}>
          <View style={styles.playDisc}>
            <Icon name="play" color="#FFFFFF" size={26} />
          </View>
        </View>
        {media.duration_ms ? (
          <View style={styles.durationBadge}>
            <AppText variant="micro" style={styles.badgeText}>
              {durationLabel(media.duration_ms)}
            </AppText>
          </View>
        ) : null}
      </Pressable>
    );
  }
  return (
    <PlayingVideo
      url={media.url}
      muted={muted}
      view={view}
      onToggleMute={() => setMuted((current) => !current)}
      onEnded={() => {
        setStage("poster");
        onStop();
      }}
    />
  );
}

/** Mounted only while a video plays, so a feed never holds a player per row. */
function PlayingVideo({ url, muted, view, onToggleMute, onEnded }: { url: string; muted: boolean; view: React.RefObject<VideoView | null>; onToggleMute(): void; onEnded(): void }) {
  const player: VideoPlayer = useVideoPlayer({ uri: url }, (instance) => {
    instance.loop = false;
    instance.muted = true;
    instance.play();
  });
  useEffect(() => {
    player.muted = muted;
  }, [player, muted]);
  const { isPlaying } = useEvent(player, "playingChange", { isPlaying: player.playing });
  useEffect(() => {
    const subscription = player.addListener("playToEnd", onEnded);
    return () => subscription.remove();
  }, [player, onEnded]);
  return (
    <View style={StyleSheet.absoluteFill}>
      <VideoView
        ref={view}
        player={player}
        style={StyleSheet.absoluteFill}
        contentFit="contain"
        nativeControls={false}
        allowsPictureInPicture={false}
        accessibilityLabel="Video"
      />
      {/* A tap on the picture goes full screen; the controls below stay out of its way. */}
      <Pressable
        style={StyleSheet.absoluteFill}
        accessibilityRole="button"
        accessibilityLabel="Full screen"
        onPress={() => {
          void Haptics.selectionAsync();
          void view.current?.enterFullscreen();
        }}
      />
      <Pressable
        style={styles.soundButton}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityState={{ selected: !muted }}
        accessibilityLabel={muted ? "Turn sound on" : "Turn sound off"}
        onPress={() => {
          void Haptics.selectionAsync();
          onToggleMute();
        }}
      >
        <Icon name={muted ? "volume-off" : "volume"} color="#FFFFFF" size={18} />
      </Pressable>
      {!isPlaying ? (
        <Pressable style={styles.centerBadge} accessibilityRole="button" accessibilityLabel="Play" onPress={() => player.play()}>
          <View style={styles.playDisc}>
            <Icon name="play" color="#FFFFFF" size={26} />
          </View>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  frame: { width: "100%", marginTop: 8, borderRadius: Radii.md, borderWidth: StyleSheet.hairlineWidth, overflow: "hidden" },
  row: { flex: 1, flexDirection: "row", gap: GAP },
  column: { flex: 1, gap: GAP },
  half: { flex: 1 },
  tile: { backgroundColor: "transparent" },
  centerBadge: { position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center" },
  playDisc: { width: 56, height: 56, borderRadius: 28, backgroundColor: "rgba(0,0,0,0.5)", alignItems: "center", justifyContent: "center", paddingLeft: 3 },
  durationBadge: { position: "absolute", right: 8, bottom: 8, borderRadius: 8, backgroundColor: "rgba(0,0,0,0.55)", paddingHorizontal: 7, paddingVertical: 2 },
  badgeText: { color: "#FFFFFF" },
  soundButton: {
    position: "absolute",
    right: 8,
    bottom: 8,
    width: 32,
    height: 32,
    borderRadius: 16,
    backgroundColor: "rgba(0,0,0,0.55)",
    alignItems: "center",
    justifyContent: "center",
  },
});
