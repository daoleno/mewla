import type { StoredServer } from "../storedServerContract";
import { AttachmentUploadCancelledError, createAttachmentUploadOperation, type UploadHost } from "./uploads";

/**
 * One upload to the current computer, for any surface that attaches a file (the
 * Session composer, the terminal key bar, More's files). The host streams the
 * bytes from the picked file to the computer's `/upload`; JS never holds them.
 * The computer keeps the file in its upload area for 7 days and answers where.
 * `/upload` takes no folder, so there is no `cwd`.
 */

/** A picked file (`files.pick`). */
export interface UploadFile {
  uri: string;
  name: string;
  mimeType?: string | null;
  size?: number | null;
}

export interface UploadOptions {
  /** Bytes sent so far, of `total` (null while the host does not know it). */
  onProgress?(sent: number, total: number | null): void;
  /** Aborting cancels the upload; it then rejects with `AttachmentUploadCancelledError`. */
  signal?: AbortSignal;
}

export interface UploadResult {
  name: string;
  /** Where the computer keeps it: what an agent is told. */
  path: string;
  /** The picked file's type (the computer does not report one). */
  content_type?: string;
}

export { AttachmentUploadCancelledError };

export async function uploadFile(host: UploadHost, server: StoredServer, file: UploadFile, options: UploadOptions = {}): Promise<UploadResult> {
  const { signal } = options;
  if (signal?.aborted) throw new AttachmentUploadCancelledError();
  const operation = createAttachmentUploadOperation(file, server, host, {
    onProgress: (progress) => options.onProgress?.(progress.transferredBytes ?? 0, progress.totalBytes),
  });
  const abort = () => void operation.cancel();
  signal?.addEventListener("abort", abort, { once: true });
  try {
    const uploaded = await operation.result;
    return uploaded.mimeType ? { name: uploaded.name, path: uploaded.path, content_type: uploaded.mimeType } : { name: uploaded.name, path: uploaded.path };
  } finally {
    signal?.removeEventListener("abort", abort);
  }
}
