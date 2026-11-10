import { codedRequest, type Daemon } from "../more/daemon";
import {
  normalizeSessionFileMetadata,
  normalizeSessionFileText,
  type SessionFileMetadata,
  type SessionFileRequest,
  type SessionFileTextPreview,
} from "./sessionFilePreview";
import { normalizeBrainWorkspaceFile, normalizeBrainWorkspaceTree, type BrainWorkspaceFile, type BrainWorkspaceTree } from "./workspace";

/** What Files asks the current computer over its socket; a refusal keeps the computer's `code`. */

const DISCONNECTED = "Server disconnected. Reconnect and try again.";

function sessionFileFields(request: SessionFileRequest): Record<string, unknown> {
  return { worker_id: request.workerId, process_id: request.processId, started_at: request.startedAt, path: request.path };
}

export async function getSessionFileMetadata(daemon: Daemon, request: SessionFileRequest): Promise<SessionFileMetadata> {
  const frame = await codedRequest<{ metadata?: unknown }>(daemon, "session_file_metadata", sessionFileFields(request), {
    reply: "session_file_metadata",
    timeoutMs: 10_000,
    timeout: "Timed out while inspecting the Session file.",
    failure: "Failed to inspect the Session file.",
    disconnected: DISCONNECTED,
  });
  return normalizeSessionFileMetadata(frame.metadata);
}

export async function getSessionFileText(daemon: Daemon, request: SessionFileRequest & { generation: string }): Promise<SessionFileTextPreview> {
  const frame = await codedRequest<{ text?: unknown }>(daemon, "session_file_text", { ...sessionFileFields(request), file_generation: request.generation }, {
    reply: "session_file_text",
    timeoutMs: 10_000,
    timeout: "Timed out while reading the Session file.",
    failure: "Failed to read the Session file.",
    disconnected: DISCONNECTED,
  });
  return normalizeSessionFileText(frame.text);
}

export async function getBrainWorkspaceTree(daemon: Daemon, path = ""): Promise<BrainWorkspaceTree> {
  const frame = await codedRequest<{ workspace_tree?: unknown }>(daemon, "brain_workspace_tree", { path }, {
    reply: "brain_workspace_tree",
    timeoutMs: 15_000,
    timeout: "Timed out while loading Brain workspace.",
    failure: "Failed to load Brain workspace.",
    disconnected: DISCONNECTED,
  });
  return normalizeBrainWorkspaceTree(frame.workspace_tree);
}

export async function getBrainWorkspaceFile(daemon: Daemon, path: string): Promise<BrainWorkspaceFile> {
  const frame = await codedRequest<{ file?: unknown }>(daemon, "brain_workspace_file", { path }, {
    reply: "brain_workspace_file",
    timeoutMs: 15_000,
    timeout: "Timed out while loading Brain workspace file.",
    failure: "Failed to load Brain workspace file.",
    disconnected: DISCONNECTED,
  });
  return normalizeBrainWorkspaceFile(frame.file);
}
