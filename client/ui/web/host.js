// The browser's host for the web build (nsh.json `web.host`): what the page has and the app's
// QuickJS runtime does not. Plain browser JavaScript, loaded by the page `nsh package --target web`
// writes; `createHost(player)` runs before the app starts and answers its `host.call`s, and sends
// it events with `player.hostEvent(name, payload)`.
//
// - secure.*: the device key, in localStorage under the keys the Expo web app used
//   (`mewla:secure:<key>`), so a browser paired before the switch stays paired.
// - app.legacyStorage / app.legacyStorageCopied: the Expo web app's other entries (its AsyncStorage
//   wrote them to localStorage as they are: computers, drafts, preferences), copied once.
// - app.origin: the page's origin, the computer's address for a browser that asks to join.
// - links.initial: the pairing link `mewla pair` gives browsers (`<origin>/#pair=<link>`), read once
//   and taken out of the address bar and history.
// - web.*: the address bar, the tab title and the browser's Back and Forward
//   (client/ui/shell/webLocation.ts; core/web/history.ts decides what each step does).
// - files.*: M4's Files contract (client/ui/more/files/host.ts) the browser's way: an
//   `<input type=file>` picker, an XHR upload with progress and abort, and a download into memory
//   handed to the browser's own download (`<a download>`), and a picked picture's thumbnail.
//
// The web player has no host posts (only calls reach the page), so the app sends `files.cancel`
// and `files.cancelDownload` as calls in the browser.

const SECURE_PREFIX = "mewla:secure:";
const LEGACY_COPIED = "mewla:web-host:legacy-copied";

function storage() {
  try {
    return window.localStorage;
  } catch {
    // Storage off (some private windows): the app keeps working for this page's life.
    return null;
  }
}

function keyOf(args) {
  const key = args && typeof args.key === "string" ? args.key : "";
  if (!key) throw new Error("secure: key is required");
  return SECURE_PREFIX + key;
}

function legacyEntries() {
  const local = storage();
  if (!local || local.getItem(LEGACY_COPIED) === "1") return null;
  const entries = {};
  for (let index = 0; index < local.length; index++) {
    const key = local.key(index);
    if (!key || !key.startsWith("mewla") || key.startsWith(SECURE_PREFIX) || key === LEGACY_COPIED) continue;
    const value = local.getItem(key);
    if (value !== null) entries[key] = value;
  }
  return Object.keys(entries).length ? entries : null;
}

function initialLink() {
  const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  if (!fragment.has("pair")) return null;
  const link = (fragment.get("pair") || "").trim();
  window.history.replaceState(window.history.state, "", window.location.pathname + window.location.search);
  return link || null;
}

// --- The address bar and history ---------------------------------------------------------------

/** The app's own state in an entry; anything else (NSH's, an old page's) is not the app's. */
function entryState(state) {
  return state && typeof state === "object" && state.mewla && typeof state.mewla === "object" ? state.mewla : null;
}

function here() {
  return window.location.pathname + window.location.search;
}

/**
 * The app owns the history: NSH's player would otherwise push an entry of its own per page of a
 * stack (`history: true` when the app is the top page, which `nsh package` cannot turn off), and
 * the two would step over each other. Its popstate listener stays; it ignores entries without its
 * `nsh` field, which the app's never have.
 */
function takeHistory(player) {
  if (player && "history" in player) player.history = false;
}

function setLocation(args) {
  const path = typeof args.path === "string" && args.path.startsWith("/") ? args.path : "/";
  // Built fresh: never NSH's fields, which would make its listener step back.
  const state = { mewla: args.state ?? null };
  switch (args.mode) {
    case "push":
      window.history.pushState(state, "", path);
      break;
    case "back":
      window.history.back();
      break;
    case "forward":
      window.history.forward();
      break;
    default:
      window.history.replaceState(state, "", path);
  }
  if (typeof args.title === "string" && args.title) document.title = args.title;
  return null;
}

function listenForSteps(player) {
  window.addEventListener("popstate", (event) => {
    player.hostEvent("web.popstate", { path: here(), state: entryState(event.state) });
  });
}

// --- Files -------------------------------------------------------------------------------------

/** The files the person picked, by the handle the app holds (`web-file:<n>`): JS never reads them. */
const picked = new Map();
let pickSequence = 0;
const uploads = new Map();
const downloads = new Map();

function remember(file) {
  pickSequence += 1;
  const uri = `web-file:${pickSequence}`;
  picked.set(uri, file);
  return { uri, name: file.name || "upload", mimeType: file.type || null, size: file.size };
}

/** `files.pick {maxCount}` → the picked files, `[]` when the picker was closed. */
function pickFiles(args) {
  const maxCount = Math.max(1, Number(args.maxCount) || 1);
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.multiple = maxCount > 1;
    input.style.display = "none";
    let settled = false;
    const finish = (files) => {
      if (settled) return;
      settled = true;
      input.remove();
      resolve(files);
    };
    input.addEventListener("change", () => finish(Array.from(input.files || []).slice(0, maxCount).map(remember)));
    // Closing the picker (Chrome 113, Firefox 91, Safari 16.4 and later). No focus-based guess:
    // focus can come back before a slow picker reports its files, and the pick would be lost.
    input.addEventListener("cancel", () => finish([]));
    document.body.appendChild(input);
    // Right away, inside the press that asked: browsers open a picker only for a person's gesture.
    input.click();
  });
}

/**
 * `files.upload {uploadId, url, fileUri, expectedSize, headers}` → `{status, body}`: the picked
 * file itself, POSTed with XHR (fetch has no upload progress). `files.uploadProgress` at most
 * every 200 ms, `totalBytes` -1 when unknown, the last one whole.
 */
function upload(player, args) {
  const file = picked.get(args.fileUri);
  if (!file) return Promise.reject(new Error("Choose the file again to upload it."));
  const uploadId = String(args.uploadId || "");
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    uploads.set(uploadId, xhr);
    let reported = 0;
    const progress = (bytesSent, totalBytes) => player.hostEvent("files.uploadProgress", { uploadId, bytesSent, totalBytes });
    const done = () => uploads.delete(uploadId);
    xhr.open("POST", args.url);
    for (const [name, value] of Object.entries(args.headers || {})) xhr.setRequestHeader(name, String(value));
    xhr.upload.onprogress = (event) => {
      const now = Date.now();
      if (now - reported < 200) return;
      reported = now;
      progress(event.loaded, event.lengthComputable ? event.total : -1);
    };
    xhr.onload = () => {
      done();
      progress(file.size, file.size);
      resolve({ status: xhr.status, body: xhr.responseText });
    };
    xhr.onerror = () => {
      done();
      reject(new Error("Upload could not reach Mewla. Check the connection and retry."));
    };
    xhr.onabort = () => {
      done();
      reject(new Error("Attachment upload cancelled."));
    };
    xhr.send(file);
  });
}

function cancelUpload(args) {
  uploads.get(String(args.uploadId || ""))?.abort();
  return null;
}

/**
 * `files.thumbnail {uri, maxPixels}` → `{dataUrl}` or null: a picked picture scaled so its longer
 * side is at most `maxPixels`, as a JPEG; null when it is not a picture this browser decodes.
 */
async function thumbnail(args) {
  const file = picked.get(args.uri);
  const maxPixels = Number(args.maxPixels) || 0;
  if (!file || !String(file.type).startsWith("image/") || maxPixels <= 0) return null;
  let bitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
  } catch {
    return null;
  }
  const factor = Math.min(1, maxPixels / Math.max(bitmap.width, bitmap.height, 1));
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(bitmap.width * factor));
  canvas.height = Math.max(1, Math.round(bitmap.height * factor));
  const context = canvas.getContext("2d");
  // A JPEG has no transparency: what was clear shows white, not black.
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  return { dataUrl: canvas.toDataURL("image/jpeg", 0.85) };
}

/** The browser saves where its settings say: there is no folder to choose. */
function pickDirectory() {
  return { token: "browser-downloads", name: "Downloads" };
}

function deliver(blob, name) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  anchor.rel = "noopener";
  anchor.style.display = "none";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  // The browser reads the blob after the click: revoke late enough that the download never breaks.
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

/**
 * `files.download {downloadId, url, names, mimeType, expectedSize, maxBytes, headers}` →
 * `{bytesWritten, name}`: the signed GET streamed into memory under `maxBytes`, checked against
 * `expectedSize`, then handed to the browser's download as the first of `names`.
 */
async function download(args) {
  const downloadId = String(args.downloadId || "");
  const controller = new AbortController();
  downloads.set(downloadId, controller);
  const maxBytes = Number(args.maxBytes) || Infinity;
  try {
    const response = await fetch(args.url, { method: "GET", headers: args.headers || {}, cache: "no-store", signal: controller.signal });
    if (!response.ok) {
      response.body?.cancel().catch(() => {});
      throw new Error(`Session file download failed (HTTP ${response.status}).`);
    }
    if (!response.body) throw new Error("Session file download returned an empty body.");
    const reader = response.body.getReader();
    const chunks = [];
    let bytesWritten = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      bytesWritten += value.byteLength;
      if (bytesWritten > maxBytes) {
        reader.cancel().catch(() => {});
        throw new Error(`Session file download exceeded the ${maxBytes} byte preview limit.`);
      }
      chunks.push(value);
    }
    if (typeof args.expectedSize === "number" && bytesWritten !== args.expectedSize) {
      throw new Error(`Session file download was truncated (expected ${args.expectedSize} bytes, received ${bytesWritten}).`);
    }
    const name = (Array.isArray(args.names) && args.names[0]) || "download";
    deliver(new Blob(chunks, { type: args.mimeType || "application/octet-stream" }), name);
    return { bytesWritten, name };
  } catch (error) {
    if (controller.signal.aborted) throw new Error("Session file download was cancelled.");
    throw error;
  } finally {
    downloads.delete(downloadId);
  }
}

function cancelDownload(args) {
  downloads.get(String(args.downloadId || ""))?.abort();
  return null;
}

export function createHost(player) {
  // `nsh test` loads this module outside a browser: the page's objects are only touched in one.
  if (typeof window !== "undefined") {
    takeHistory(player);
    if (player) listenForSteps(player);
  }
  const handlers = {
    "secure.get": (args) => storage()?.getItem(keyOf(args)) ?? null,
    "secure.set": (args) => {
      storage()?.setItem(keyOf(args), String(args.value ?? ""));
      return null;
    },
    "secure.remove": (args) => {
      storage()?.removeItem(keyOf(args));
      return null;
    },
    "app.legacyStorage": () => legacyEntries(),
    "app.legacyStorageCopied": () => {
      storage()?.setItem(LEGACY_COPIED, "1");
      return null;
    },
    "app.origin": () => window.location.origin,
    "links.initial": () => initialLink(),
    "web.initialPath": () => ({ path: here(), state: entryState(window.history.state) }),
    "web.setLocation": (args) => setLocation(args),
    "files.pick": (args) => pickFiles(args),
    "files.upload": (args) => upload(player, args),
    "files.cancel": (args) => cancelUpload(args),
    "files.pickDirectory": () => pickDirectory(),
    "files.download": (args) => download(args),
    "files.cancelDownload": (args) => cancelDownload(args),
    "files.thumbnail": (args) => thumbnail(args),
  };
  return {
    call(name, args) {
      const handler = handlers[name];
      if (!handler) throw new Error(`${name} is not available in the browser`);
      return handler(args ?? {});
    },
  };
}
