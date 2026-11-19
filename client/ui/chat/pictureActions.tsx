import { announce, Clipboard, host, platform, signal, Text, View } from "nsh";
import { dataUrlMediaType, isDirectPicture, pictureFailure, pictureFileName, type PictureAction } from "../../core/files/pictureSave";
import { showAlert } from "../shell/alert";
import type { PictureLoader } from "./ChatImage";

/**
 * Save, Share and Copy of a picture a chat shows (SPEC §1.7): always the original's bytes, the
 * computer's file read whole (`load(path, true)`, the viewer's own read, kept by chatImage), or a web
 * or data URL as it is; never the phone-sized copy a thread's row shows, never a capture of the view.
 *
 * The host gets the original as it holds it, `source` (a data URL, or a web address it downloads), so the
 * bytes are decoded natively, never in JS:
 * - `files.saveImage {source, name, mimeType}` → `{name, where}`: Android puts them in MediaStore
 *   Pictures/Mewla, iOS in Photos, a browser downloads them (client/ui/web/host.js).
 * - `files.shareImage {source, name, mimeType}` (phones): the file, staged in the app's cache, in the
 *   system share sheet.
 * - `files.canSaveImage` → true: the host has both (see picturesSupported).
 * Copy puts the picture on the clipboard where the platform has one for images (not Android).
 */

/** A picture as a chat names it: its source and its alt text. */
export type ChatPicture = { src: string; alt?: string };

const mobile = platform.os === "android" || platform.os === "ios";

/** The original's URL: a web or data URL as it is, a computer path read whole. */
export async function originalPicture(picture: ChatPicture, load?: PictureLoader): Promise<string> {
  if (isDirectPicture(picture.src)) return picture.src;
  if (!load) throw new Error("No computer to read it from.");
  const url = await load(picture.src.replace(/^file:\/\//i, ""), true);
  if (!url) throw new Error("The computer has no picture there.");
  return url;
}

/** What the host is handed: the original, its file name and (for a data URL) its type. */
async function handOver(picture: ChatPicture, load?: PictureLoader): Promise<{ source: string; name: string; mimeType: string }> {
  const source = await originalPicture(picture, load);
  const mimeType = dataUrlMediaType(source);
  return { source, name: pictureFileName(picture.src, mimeType), mimeType };
}

/** What a picture action ended with: shown for a moment where it was asked (PictureNote). */
export type PictureNoteValue = { text: string; tone: "done" | "failed" };
export const pictureNote = signal<PictureNoteValue | null>(null);
let noteTimer: ReturnType<typeof setTimeout> | undefined;

function note(text: string, tone: PictureNoteValue["tone"]): void {
  pictureNote.set({ text, tone });
  announce(text);
  clearTimeout(noteTimer);
  noteTimer = setTimeout(() => pictureNote.set(null), tone === "failed" ? 4000 : 1800);
}

/** Puts the original in the phone's gallery, or downloads it in a browser. */
export async function savePicture(picture: ChatPicture, load?: PictureLoader): Promise<boolean> {
  try {
    const saved = await host.call<{ name?: string; where?: string } | null>("files.saveImage", await handOver(picture, load), { timeout: 0 });
    note(saved?.where ? `Saved to ${saved.where}` : mobile ? "Saved" : "Downloaded", "done");
    return true;
  } catch (error) {
    note(pictureFailure("save", error), "failed");
    return false;
  }
}

/** The system share sheet with the original as a file. */
export async function sharePicture(picture: ChatPicture, load?: PictureLoader): Promise<boolean> {
  try {
    await host.call("files.shareImage", await handOver(picture, load), { timeout: 0 });
    return true;
  } catch (error) {
    note(pictureFailure("share", error), "failed");
    return false;
  }
}

/** The original on the clipboard. */
export async function copyPicture(picture: ChatPicture, load?: PictureLoader): Promise<boolean> {
  try {
    await Clipboard.writeImage(await originalPicture(picture, load));
    note("Copied", "done");
    return true;
  } catch (error) {
    note(pictureFailure("copy", error), "failed");
    return false;
  }
}

/** Which actions this platform has: Share on a phone, Copy where the clipboard holds images (not Android). */
export const pictureCan = {
  share: mobile,
  copy: platform.os !== "android",
};

/**
 * Whether this app build's host saves and shares pictures (`files.canSaveImage` → true): the UI reaches a phone
 * before the app build that has the calls (and the share provider) does, and a build from before shows no
 * Save or Share rather than ones that fail. Asked once, when a picture is first shown or held.
 */
export const picturesSupported = signal(false);
let supportAsked: Promise<boolean> | undefined;

export function askPictureSupport(): Promise<boolean> {
  supportAsked ??= host.call<boolean>("files.canSaveImage").then(
    (answer) => answer === true,
    () => false,
  ).then((supported) => (picturesSupported.set(supported), supported));
  return supportAsked;
}

/** A phone saves to its gallery; a browser downloads. */
export const saveLabel = mobile ? "Save" : "Download";

/** What the last picture action ended with, for a moment: a pill at the foot of where it was asked. */
export function PictureNote(props: { testID?: string; class?: string }) {
  return (
    <If when={pictureNote()}><View class={`items-center ${props.class ?? ""}`}>
        <View testID={props.testID ?? "picture-note"} role="alert" onPress={() => pictureNote.set(null)} class="rounded-full bg-foreground px-4 py-2.5">
          <Text class="text-center text-sub font-semibold text-background">{pictureNote()?.text ?? ""}</Text>
        </View>
      </View></If>
  );
}

/** One of the picture actions, by name. */
export async function runPictureAction(action: PictureAction, picture: ChatPicture, load?: PictureLoader): Promise<boolean> {
  if (action === "save") return savePicture(picture, load);
  if (action === "share") return sharePicture(picture, load);
  return copyPicture(picture, load);
}

/** A hold on a picture in a thread: Save and Share (and Copy where it works), as the viewer's top bar has them. */
export async function pictureMenu(picture: ChatPicture, load?: PictureLoader): Promise<void> {
  if (!(await askPictureSupport())) return;
  const actions = [
    { id: "save", label: saveLabel },
    ...(pictureCan.share ? [{ id: "share", label: "Share" }] : []),
    ...(pictureCan.copy ? [{ id: "copy", label: "Copy" }] : []),
    { id: "cancel", label: "Cancel", role: "cancel" as const },
  ];
  const answer = await showAlert({ presentation: "actionSheet", actions });
  if (answer.action === "save" || answer.action === "share" || answer.action === "copy") await runPictureAction(answer.action, picture, load);
}
