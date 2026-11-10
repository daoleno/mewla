import type { SessionFileIdentity } from "../../../core/files/sessionFilePreview";
import type { PageNav } from "../routes";
import { FilePreviewPage } from "./FilePreview";
import { filePreviewRoute, type FilesPage } from "./routes";
import { WorkspacePage } from "./Workspace";

export { AttachmentTray } from "./Attachments";
export { createAttachmentUploads, type AttachmentUploads } from "./upload";
export { filePreviewRoute, workspaceRoute, type FilesPage } from "./routes";

/** The page of a Files route, in whichever stack pushed it. */
export function FilesPageView(props: { route: FilesPage; nav: PageNav }) {
  const route = props.route;
  switch (route.name) {
    case "files-preview": return <FilePreviewPage reference={route.reference} identity={route.identity} nav={props.nav} />;
    case "files-workspace": return <WorkspacePage trail={route.trail} file={route.file} hostSession={route.hostSession} nav={props.nav} />;
  }
}

/** Opens a file a Session showed (a Markdown link, a path in its output) on `nav`. */
export function openFilePreview(nav: PageNav, reference: string, identity: SessionFileIdentity): void {
  nav.push(filePreviewRoute(reference, identity));
}
