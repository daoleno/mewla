import type { UploadProgress } from "expo-file-system";
import type { NativeUploadResult } from "../modules/zen-file-upload/src";

export interface WebUploadTask {
  uploadAsync(): Promise<NativeUploadResult>;
  cancel(): void;
  release(): void;
}

/** Native platforms upload through expo-file-system or the Android module. */
export function createWebUploadTask(_input: {
  file?: Blob;
  uploadUrl: string;
  headers: Record<string, string>;
  onProgress(progress: UploadProgress): void;
}): WebUploadTask | null {
  return null;
}
