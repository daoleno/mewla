import type {
  SessionFileDownloadBackend,
  SessionFileDownloadDirectory,
  SessionFileOwnedDestination,
} from "./sessionFilePreviewDownload";
import { SESSION_FILE_BINARY_LIMIT_BYTES } from "./sessionFilePreviewDownload";
import {
  streamSessionFileDownloadToOwnedSink,
  type SessionFileDownloadFetch,
} from "./sessionFilePreviewDownloadStream";

/**
 * Hands a fully validated download to the browser.
 * Injectable so the object-URL anchor path stays out of unit tests.
 */
export interface WebSessionFileDownloadSink {
  deliver(blob: Blob, fileName: string): void;
}

interface WebDownloadTarget {
  readonly fileName: string;
  readonly mimeType: string;
  readonly chunks: Uint8Array[];
}

const targets = new WeakMap<SessionFileOwnedDestination, WebDownloadTarget>();

/**
 * The authenticated GET must keep the capability query and the no-store
 * header; the browser exposes the response body as a ReadableStream so the
 * shared stream helper can enforce the download size bound before delivery.
 */
const browserSessionFileFetch: SessionFileDownloadFetch = async (url, init) => {
  const response = await fetch(url, {
    method: init.method,
    headers: init.headers,
  });
  return {
    ok: response.ok,
    status: response.status,
    body: response.body,
  };
};

function deliverViaAnchor(blob: Blob, fileName: string) {
  const objectURL = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = objectURL;
  anchor.download = fileName;
  anchor.rel = "noopener";
  anchor.style.display = "none";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  // The browser reads the blob asynchronously after click(), so revoke late
  // enough that the download can never be cancelled mid-flight.
  setTimeout(() => URL.revokeObjectURL(objectURL), 60_000);
}

const browserDownloadSink: WebSessionFileDownloadSink = {
  deliver: deliverViaAnchor,
};

/**
 * Production web wiring for Session file downloads.
 *
 * Browsers expose neither Android SAF nor Expo's Directory API, so the
 * authenticated response is buffered in memory under the same size bound and
 * then handed to the browser's normal object-URL anchor download.
 */
export function createSessionFileDownloadBackend(
  sink: WebSessionFileDownloadSink = browserDownloadSink,
): SessionFileDownloadBackend {
  let sequence = 0;

  return {
    async pickDirectory(): Promise<SessionFileDownloadDirectory> {
      return {
        reserve(fileName, mimeType): SessionFileOwnedDestination {
          sequence += 1;
          const target: WebDownloadTarget = {
            fileName,
            mimeType,
            chunks: [],
          };
          const destination: SessionFileOwnedDestination = {
            uri: `mewla-web-download:${sequence}`,
            delete() {
              target.chunks.length = 0;
              targets.delete(destination);
            },
            writableStream() {
              return new WritableStream<Uint8Array>({
                write(chunk) {
                  target.chunks.push(chunk);
                },
              });
            },
          };
          targets.set(destination, target);
          return destination;
        },
      };
    },
    async download(uri, destination, options) {
      await streamSessionFileDownloadToOwnedSink(
        uri,
        options.headers,
        destination,
        {
          fetch: browserSessionFileFetch,
          maxBytes: SESSION_FILE_BINARY_LIMIT_BYTES,
          expectedBytes: options.expectedBytes,
        },
      );
      const target = targets.get(destination);
      if (!target) {
        throw new Error("The browser download destination is unavailable.");
      }
      sink.deliver(
        new Blob(target.chunks as BlobPart[], { type: target.mimeType }),
        target.fileName,
      );
    },
  };
}
