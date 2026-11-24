/** From the Expo app's services/chatComposerPresentation.ts. */
import type { WorkerConnectionState as ConnectionState } from '../sessions/workers';
import type { AgentKind } from '../sessions/workerPresentation';

export function buildChatComposerPlaceholder({
  agentKind,
  connectionState,
  explicitPlaceholder,
}: {
  agentKind: AgentKind;
  connectionState: ConnectionState;
  explicitPlaceholder?: string;
}): string {
  if (explicitPlaceholder?.trim()) {
    return explicitPlaceholder.trim();
  }
  if (connectionState !== 'connected') {
    return connectionState === 'connecting'
      ? 'Connecting…'
      : 'Connection unavailable';
  }
  switch (agentKind) {
    case 'codex':
      return 'Message Codex';
    case 'grok':
      return 'Message Grok';
    case 'cursor':
      return 'Message Cursor';
    case 'claude':
      return 'Message Claude';
    case 'antigravity':
      return 'Message Antigravity';
    default:
      return 'Message';
  }
}
