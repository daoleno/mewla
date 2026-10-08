import { requireNativeModule, type EventSubscription } from "expo-modules-core";

export interface NativeUploadProgress {
  uploadId: string;
  bytesSent: number;
  totalBytes: number;
}

export interface NativeUploadResult {
  body: string;
  status: number;
  headers: Record<string, string>;
}

export interface NativeUploadRequest {
  uploadId: string;
  url: string;
  fileUri: string;
  expectedSize: number | null;
  method: string;
  headers: Record<string, string>;
}

export interface NativeDownloadResult {
  bytesWritten: number;
}

export interface NativeDownloadRequest {
  downloadId: string;
  url: string;
  destinationUri: string;
  expectedSize: number | null;
  maxBytes: number;
  headers: Record<string, string>;
}

export interface NativePickedDocument {
  selectionError?: string;
  selectionRetryable?: boolean;
  uri: string;
  name: string;
  mimeType: string;
  size: number | null;
}

interface FileUploadNativeModule {
  pickDocuments?(maxCount: number): Promise<NativePickedDocument[]>;
  upload(request: NativeUploadRequest): Promise<NativeUploadResult>;
  cancel(uploadId: string): boolean;
  download?(request: NativeDownloadRequest): Promise<NativeDownloadResult>;
  cancelDownload?(downloadId: string): boolean;
  addListener(
    eventName: "onUploadProgress",
    listener: (progress: NativeUploadProgress) => void,
  ): EventSubscription;
}

let cached: FileUploadNativeModule | null | undefined;

export function getFileUploadModule(): FileUploadNativeModule | null {
  if (cached === undefined) {
    try {
      cached = requireNativeModule<FileUploadNativeModule>("FileUpload");
    } catch {
      cached = null;
    }
  }
  return cached;
}

export function getFileDownloadModule(): FileUploadNativeModule | null {
  const module = getFileUploadModule();
  return module && typeof module.download === "function" ? module : null;
}
