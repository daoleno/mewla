export const PAIRING_SCOPE_COPY = "Pairing grants this phone access to this Zen server, including sessions, terminal, Brain, Workers, and files. Only pair a phone you trust.";

export class PairingCancelledError extends Error {
  constructor() { super("Pairing cancelled."); this.name = "PairingCancelledError"; }
}
