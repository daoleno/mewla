import type { UploadProgress } from "expo-file-system";
import type { NativeUploadResult } from "../modules/zen-file-upload/src";
import type { WebUploadTask } from "./webUploadTask";

export type { WebUploadTask } from "./webUploadTask";

/**
 * expo-file-system's web upload task is a stub that resolves with status 0.
 * Browsers send the picked File itself; XHR keeps byte progress and abort.
 */
export function createWebUploadTask(input: {
  file?: Blob;
  uploadUrl: string;
  headers: Record<string, string>;
  onProgress(progress: UploadProgress): void;
}): WebUploadTask {
  const xhr = new XMLHttpRequest();
  return {
    uploadAsync: () =>
      new Promise<NativeUploadResult>((resolve, reject) => {
        if (!input.file) {
          reject(new Error("Choose the file again to upload it."));
          return;
        }
        xhr.open("POST", input.uploadUrl);
        Object.entries(input.headers).forEach(([name, value]) =>
          xhr.setRequestHeader(name, value),
        );
        xhr.upload.onprogress = (event) =>
          input.onProgress({
            bytesSent: event.loaded,
            totalBytes: event.lengthComputable ? event.total : 0,
          });
        xhr.onload = () =>
          resolve({ status: xhr.status, body: xhr.responseText, headers: {} });
        xhr.onerror = () =>
          reject(new Error("Upload could not reach Mewla. Check the connection and retry."));
        xhr.onabort = () => reject(new Error("Attachment upload cancelled."));
        xhr.send(input.file);
      }),
    cancel: () => xhr.abort(),
    release() {},
  };
}
