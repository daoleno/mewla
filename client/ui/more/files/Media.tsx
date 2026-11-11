import { cleanup, signal, Video, View } from "nsh";
import type { VideoRef } from "nsh";
import { createSessionFileMediaRecovery, type SessionFileMediaKind } from "../../../core/files/sessionFileMedia";
import { isStaleSessionFileError, type SessionFileBinarySource } from "../../../core/files/sessionFilePreview";

/**
 * A Session file's video or audio, streamed through its read capability with the
 * platform's controls. The capability lapses after 2 minutes, so a seek or a resume
 * later fails: the player then signs a fresh one and goes on from where it was
 * (`createSessionFileMediaRecovery`); what it can't recover from is `onFail`'s.
 */
export function MediaPlayer(props: {
  kind: SessionFileMediaKind;
  source: SessionFileBinarySource;
  resolve: () => Promise<SessionFileBinarySource>;
  onFail: (message: string, stale: boolean) => void;
  testID?: string;
}) {
  const src = signal(props.source.uri);
  const paused = signal(true);
  let view: VideoRef | null = null;
  let resumeAt = 0;
  const recovery = createSessionFileMediaRecovery({
    kind: props.kind,
    resolve: props.resolve,
    load: (next, at, play) => {
      resumeAt = at;
      src.set(next.uri);
      paused.set(!play);
    },
    fail: (message, stale) => props.onFail(message, stale),
    isStale: isStaleSessionFileError,
  });
  cleanup(() => recovery.dispose());
  return (
    <View class="flex-1 items-center justify-center bg-background p-4">
      <Video
        testID={props.testID}
        ref={(ref) => (view = ref)}
        src={src()}
        controls
        paused={paused()}
        class={props.kind === "video" ? "aspect-video w-full" : "h-16 w-full"}
        accessibilityLabel={props.kind === "video" ? "Video preview" : "Audio preview"}
        onLoad={() => {
          recovery.loaded();
          if (resumeAt > 0) view?.seek(resumeAt);
          resumeAt = 0;
        }}
        onProgress={(event) => recovery.progress(event.currentTime)}
        onEnd={() => recovery.ended()}
        onError={() => recovery.error()}
      />
    </View>
  );
}
