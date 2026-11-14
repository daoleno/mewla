/** From the Expo app's services/chatComposerPresentation.ts. */
import type { WorkerConnectionState as ConnectionState } from '../sessions/workers';
import type { AgentKind } from '../sessions/workerPresentation';

export function chatAgentSupportsSlashCommands(kind: AgentKind): boolean {
  return kind === 'codex';
}

export function buildChatComposerPlaceholder({
  agentKind,
  connectionState,
  slashQueryActive,
  explicitPlaceholder,
}: {
  agentKind: AgentKind;
  connectionState: ConnectionState;
  slashQueryActive: boolean;
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
  if (slashQueryActive && chatAgentSupportsSlashCommands(agentKind)) {
    return 'Search commands';
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
    default:
      return 'Message';
  }
}
