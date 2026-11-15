/** From the Expo app's services/sessionKeys.ts. A Session's device-local state is keyed by it. */
export function makeSessionKey(serverId: string, workerId: string): string {
  return JSON.stringify([serverId, workerId]);
}
