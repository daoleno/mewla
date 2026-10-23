import type { Worker } from '../store/workers';
import { displayPathSubtitle, pathBasename } from './pathDisplay';
import {
  detectTerminalFlavor,
  terminalFlavorLabel,
  type TerminalFlavor,
} from './terminalFlavor';

export type AgentKind = 'terminal' | 'claude' | 'codex' | 'cursor' | 'grok' | 'pi' | 'opencode' | 'dsh';
export type WorkerTitleSource = 'alias' | 'explicit_name' | 'default';
export type { TerminalFlavor };

export type PresentedWorker = {
  kind: AgentKind;
  terminalFlavor: TerminalFlavor;
  title: string;
  shortTitle: string;
  subtitle: string;
  typeLabel: string;
  cwdBase: string;
  titleSource: WorkerTitleSource;
};

export function presentWorker(agent: Pick<Worker, 'name' | 'project' | 'cwd' | 'command' | 'provider' | 'summary' | 'last_output_lines'>, alias?: string): PresentedWorker {
  const kind = agentKindFromProvider(agent.provider);
  const terminalFlavor =
    kind === 'terminal' ? detectTerminalFlavor(agent) : 'shell';
  const label = typeLabel(kind, terminalFlavor);
  const cwd = normalize(agent.cwd);
  const cwdBase = pathBasename(cwd);
  const project = normalize(agent.project);
  const cleanName = sanitizeName(agent.name);
  const explicitAlias = normalize(alias);
  const location = project || cwdBase;
  const fallbackTitle = location || defaultTitle(kind);

  if (explicitAlias) {
    return {
      kind,
      terminalFlavor,
      title: explicitAlias,
      shortTitle: explicitAlias,
      subtitle: buildSubtitle(label, cwd || project),
      typeLabel: label,
      cwdBase,
      titleSource: 'alias',
    };
  }

  const hasWorkerTitle = cleanName && !isGenericWorkerTitle(cleanName, kind);
  const title = hasWorkerTitle ? cleanName : fallbackTitle;

  return {
    kind,
    terminalFlavor,
    title,
    shortTitle: hasWorkerTitle ? title : (location || shortDefaultTitle(kind)),
    subtitle: buildSubtitle(label, location || cwd),
    typeLabel: label,
    cwdBase,
    titleSource: hasWorkerTitle ? 'explicit_name' : 'default',
  };
}

const AGENT_KINDS: ReadonlySet<string> = new Set<AgentKind>(['claude', 'codex', 'cursor', 'grok', 'pi', 'opencode', 'dsh']);

/**
 * The kind to present a Worker as. The daemon names the provider of the
 * Worker's executor; a Worker with none (a plain terminal, or a provider this
 * app has no art for) is a terminal. The command line is never read.
 */
export function agentKindFromProvider(provider?: string): AgentKind {
  return provider && AGENT_KINDS.has(provider) ? (provider as AgentKind) : 'terminal';
}

function isGenericWorkerTitle(name: string, kind: AgentKind): boolean {
  if (!name) return true;
  const lower = name.toLowerCase().replace(/\s+/g, ' ').trim();
  if (
    kind === 'claude' && (
      lower === 'claude' ||
      lower === 'claude code' ||
      lower === 'claude-code'
    )
  ) return true;
  if (kind === 'codex' && (lower === 'codex' || lower === 'openai codex')) return true;
  if (kind === 'cursor' && (lower === 'agent' || lower === 'cursor' || lower === 'cursor agent')) return true;
  if (kind === 'grok' && (lower === 'grok' || lower === 'grok cli' || lower === 'xai grok')) return true;
  if (kind === 'pi' && (lower === 'pi' || lower === 'pi coding agent')) return true;
  if (kind === 'opencode' && (lower === 'opencode' || lower === 'open code')) return true;
  if (
    lower === 'zsh' ||
    lower === 'bash' ||
    lower === 'sh' ||
    lower === 'fish' ||
    lower === 'shell' ||
    lower === 'terminal' ||
    lower === 'tmux' ||
    lower === '[tmux]' ||
    lower === 'node' ||
    lower === 'bun' ||
    lower === 'python' ||
    lower === 'python3' ||
    lower.includes('tmux') ||
    lower.startsWith('./') ||
    lower.startsWith('/')
  ) return true;
  return /^[\w.-]+:[@%\w.-]+$/.test(lower);
}

function sanitizeName(value?: string): string {
  const trimmed = normalize(value);
  if (!trimmed) return '';
  return trimmed.replace(/\s+\([^)]+\)\s*$/, '').trim();
}

function normalize(value?: string): string {
  return value?.trim() || '';
}

function defaultTitle(kind: AgentKind): string {
  switch (kind) {
    case 'claude':
      return 'Claude';
    case 'codex':
      return 'Codex';
    case 'cursor':
      return 'Cursor Agent';
    case 'grok':
      return 'Grok';
    case 'pi':
      return 'Pi';
    case 'dsh': return 'DSH';
    case 'opencode':
      return 'OpenCode';
    default:
      return 'Shell';
  }
}

function shortDefaultTitle(kind: AgentKind): string {
  switch (kind) {
    case 'claude':
      return 'Claude';
    case 'codex':
      return 'Codex';
    case 'cursor':
      return 'Cursor';
    case 'grok':
      return 'Grok';
    case 'pi':
      return 'Pi';
    case 'dsh': return 'DSH';
    case 'opencode':
      return 'OpenCode';
    default:
      return 'Shell';
  }
}

function typeLabel(kind: AgentKind, terminalFlavor: TerminalFlavor): string {
  switch (kind) {
    case 'claude':
      return 'Claude Code';
    case 'codex':
      return 'OpenAI Codex';
    case 'cursor':
      return 'Cursor Agent';
    case 'grok':
      return 'Grok';
    case 'pi':
      return 'Pi';
    case 'dsh': return 'DSH';
    case 'opencode':
      return 'OpenCode';
    default:
      return terminalFlavorLabel(terminalFlavor);
  }
}

function buildSubtitle(label: string, location: string): string {
  const compactLocation = displayPathSubtitle(location);
  return [label, compactLocation].filter(Boolean).join(' · ');
}
