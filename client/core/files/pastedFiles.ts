const PASTED_NAME_EXTENSION: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
  "image/webp": "webp",
  "image/svg+xml": "svg",
  "image/heic": "heic",
  "application/pdf": "pdf",
  "text/plain": "txt",
};

const two = (n: number) => String(n).padStart(2, "0");

/**
 * The name a pasted or dropped file is uploaded under. A clipboard picture comes as
 * the generic "image.png" (or with no name), so it is named by when it was pasted:
 * `Pasted image 2026-11-11 08.05.09.png`; any other name is kept (v0.3.1 web).
 */
export function pastedFileName(name: string, type: string, now: Date = new Date()): string {
  const generic = !name || /^image\.[a-z0-9]+$/i.test(name);
  if (!generic) return name;
  const extension = PASTED_NAME_EXTENSION[type] ?? "";
  const stamp = `${now.getFullYear()}-${two(now.getMonth() + 1)}-${two(now.getDate())} ${two(now.getHours())}.${two(now.getMinutes())}.${two(now.getSeconds())}`;
  return `Pasted ${type.startsWith("image/") ? "image" : "file"} ${stamp}${extension ? `.${extension}` : ""}`;
}
