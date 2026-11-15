import type { PageNav } from "../routes";
import { FilePreviewPage } from "./FilePreview";
import type { FilesPage } from "./routes";
import { WorkspacePage } from "./Workspace";

export { AttachmentTray } from "./Attachments";
export { createAttachmentUploads } from "./upload";
export { type FilesPage } from "./routes";

/** The page of a Files route, in whichever stack pushed it. */
export function FilesPageView(props: { route: FilesPage; nav: PageNav }) {
  const route = props.route;
  switch (route.name) {
    case "files-preview": return <FilePreviewPage reference={route.reference} identity={route.identity} nav={props.nav} />;
    case "files-workspace": return <WorkspacePage trail={route.trail} file={route.file} hostSession={route.hostSession} nav={props.nav} />;
  }
}
