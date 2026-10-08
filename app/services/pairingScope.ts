export type PairingDevice = "browser" | "phone";

/** Name the device being paired: the browser on web, the phone in the app. */
export function pairingScopeCopy(device: PairingDevice): { title: string; message: string } {
  return {
    title: `Pair this ${device}?`,
    message: `Pairing gives this ${device} access to this Mewla server, including sessions, terminal, Brain, Workers, and files. Only pair a ${device} you trust.`,
  };
}

export class PairingCancelledError extends Error {
  constructor() { super("Pairing cancelled."); this.name = "PairingCancelledError"; }
}
