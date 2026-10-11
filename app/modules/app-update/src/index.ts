import { requireNativeModule, type EventSubscription } from "expo-modules-core";

export interface AppUpdateDownloadRequest {
  url: string;
  fileName: string;
  sha256: string;
  size: number;
}

export interface AppUpdateProgress {
  received: number;
  total: number;
}

interface AppUpdateNativeModule {
  download(request: AppUpdateDownloadRequest): Promise<{ fileName: string }>;
  cancelDownload(): boolean;
  canInstallPackages(): boolean;
  openInstallSettings(): Promise<void>;
  install(fileName: string): Promise<void>;
  addListener(
    eventName: "onDownloadProgress",
    listener: (progress: AppUpdateProgress) => void,
  ): EventSubscription;
}

let cached: AppUpdateNativeModule | null | undefined;

/** Android only; iOS has no native counterpart and gets null. */
export function getAppUpdateModule(): AppUpdateNativeModule | null {
  if (cached === undefined) {
    try {
      cached = requireNativeModule<AppUpdateNativeModule>("AppUpdate");
    } catch {
      cached = null;
    }
  }
  return cached;
}
