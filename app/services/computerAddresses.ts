import type { StoredServer } from "./storedServerContract";

// otherComputerURLs are the addresses of `other` that another computer could
// dial. The phone's own Link tunnel is local to the phone and never passed.
export function otherComputerURLs(other: StoredServer): string[] {
  const urls: string[] = [];
  const add = (value: string | undefined) => {
    const trimmed = value?.trim();
    if (!trimmed) return;
    try {
      const parsed = new URL(trimmed);
      if (parsed.protocol === "ws:") parsed.protocol = "http:";
      if (parsed.protocol === "wss:") parsed.protocol = "https:";
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return;
      const base = `${parsed.protocol}//${parsed.host}`;
      if (!urls.includes(base)) urls.push(base);
    } catch {
      // Not a URL; skip it.
    }
  };
  if (other.transportKind !== "link") add(other.url);
  for (const candidate of other.transportCandidates ?? []) {
    if (candidate.kind === "manual") add(candidate.url);
  }
  return urls;
}
