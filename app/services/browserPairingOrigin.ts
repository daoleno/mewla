export interface BrowserPairingTarget {
  url: string;
  daemonId?: string;
  daemonPublicKey: string;
}

/** Native apps reach the daemon at the URL the pairing link names. */
export async function resolveBrowserPairingURL(
  target: BrowserPairingTarget,
): Promise<string> {
  return target.url;
}
