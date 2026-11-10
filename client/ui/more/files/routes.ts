import type { SessionFileIdentity } from "../../../core/files/sessionFilePreview";

/** Files' pages: a Session file's preview, and Brain's workspace. */
export type FilesPage =
  | { name: "files-preview"; reference: string; identity: SessionFileIdentity }
  /** A folder of it (`trail`: folder paths, outermost first), or one file (`file`). `hostSession`: Brain's own Session, which plays its media. */
  | { name: "files-workspace"; trail?: string[]; file?: string; hostSession?: SessionFileIdentity };

/** A file reference a Session showed (a link, a path in its output), previewed against that live Session. */
export function filePreviewRoute(reference: string, identity: SessionFileIdentity): FilesPage {
  return { name: "files-preview", reference, identity };
}

/** Brain's workspace on the current computer. */
export function workspaceRoute(hostSession?: SessionFileIdentity | null): FilesPage {
  return hostSession ? { name: "files-workspace", hostSession } : { name: "files-workspace" };
}
