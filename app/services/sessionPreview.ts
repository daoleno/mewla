import type { Worker } from '../store/workers';
import { stripAnsiText } from './ansiText';

export type SessionPreviewTone = 'default' | 'muted' | 'accent' | 'danger' | 'success' | 'needs';

export type SessionPreview = {
  text: string;
  tone: SessionPreviewTone;
  prefix?: string;
};

export function formatWorkerSessionPreview(
  agent: Pick<
    Worker,
    | 'status'
    | 'summary'
    | 'attention'
    | 'last_output_lines'
    | 'delegated'
    | 'needs_attention'
    | 'phase'
  >,
  options?: { showServerName?: boolean; serverName?: string },
): SessionPreview {
  const serverPrefix = options?.showServerName && options.serverName
    ? `${options.serverName}: `
    : undefined;
  const summary = agent.summary?.trim();
  // A delegated Worker reports what it is doing through `mewla worker
  // progress`; that structured summary beats whatever its terminal printed
  // last (often the provider's status bar).
  const lastLine = (agent.delegated && summary) || extractLastOutputLine(agent.last_output_lines);

  if (agent.status === 'running') {
    if (agent.delegated) {
      return {
        text: lastLine || summary || 'No recent output',
        tone: 'accent',
        prefix: serverPrefix,
      };
    }
    if (lastLine) {
      return {
        text: lastLine,
        tone: 'default',
        prefix: serverPrefix,
      };
    }
    if (summary) {
      return {
        text: summary,
        tone: 'accent',
        prefix: serverPrefix,
      };
    }
    return {
      text: 'No recent output',
      tone: 'accent',
      prefix: serverPrefix,
    };
  }

  if (agent.status === 'blocked' || agent.status === 'failed') {
    const detail = agent.attention?.trim() || agent.phase?.trim();
    // Seal & Slip: only failure is oxblood; a waiting Session is the seal
    // when it waits on you, and plain otherwise.
    return {
      text: lastLine || summary || detail || 'No recent output',
      tone: agent.status === 'failed' ? 'danger' : agent.needs_attention ? 'needs' : 'default',
      prefix: serverPrefix,
    };
  }

  if (agent.status === 'done') {
    return {
      text: lastLine || summary || 'No recent output',
      tone: 'muted',
      prefix: serverPrefix,
    };
  }

  if (agent.needs_attention) {
    return {
      text: agent.attention?.trim() || 'Waiting for input',
      tone: 'needs',
      prefix: serverPrefix,
    };
  }

  return {
    text: lastLine || summary || 'No recent output',
    tone: 'muted',
    prefix: serverPrefix,
  };
}

function extractLastOutputLine(lines: string[]): string {
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    const cleaned = stripAnsiText(lines[index] ?? '');
    if (!cleaned || isNoiseLine(cleaned)) {
      continue;
    }
    return truncatePreview(cleaned);
  }
  return '';
}

function isNoiseLine(line: string): boolean {
  const normalized = line.trim();
  if (!normalized) {
    return true;
  }
  if (/^[\-_─═│┌┐└┘├┤┬┴┼╭╮╯╰╱╲╳\s]+$/.test(normalized)) {
    return true;
  }
  if (/^[\$>#%]\s*$/.test(normalized)) {
    return true;
  }
  return false;
}

function truncatePreview(value: string, max = 72): string {
  const compact = value.replace(/\s+/g, ' ').trim();
  if (compact.length <= max) {
    return compact;
  }
  return `${compact.slice(0, max - 1)}…`;
}
