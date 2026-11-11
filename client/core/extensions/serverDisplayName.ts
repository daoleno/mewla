// A server added by address is named by it ("127.0.0.1"). Sentences that say
// where something is kept need a name a person would use.
const ADDRESS = /^(localhost|\d{1,3}(\.\d{1,3}){3}|\[?[0-9a-f]*:[0-9a-f:.]*\]?)(:\d+)?$/i;

export function serverDisplayName(name: string | null | undefined, fallback = "this computer"): string {
  const trimmed = name?.trim();
  return trimmed && !ADDRESS.test(trimmed) ? trimmed : fallback;
}
