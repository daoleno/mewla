import type { Worker } from '../store/workers';
import { commandBinary } from './agentCommands';

export type TerminalFlavor =
  | 'shell'
  | 'ssh'
  | 'go'
  | 'rust'
  | 'python'
  | 'node'
  | 'typescript'
  | 'bun'
  | 'docker'
  | 'kubernetes'
  | 'postgres'
  | 'redis'
  | 'nginx'
  | 'git'
  | 'java'
  | 'ruby'
  | 'php'
  | 'terraform'
  | 'aws'
  | 'bash';

// The icon follows the program in the foreground: the binary the Worker's
// command starts. Names, paths, summaries and pane output say nothing reliable
// about it, so they are not consulted.
const FLAVOR_BY_BINARY: Record<string, TerminalFlavor> = {
  ssh: 'ssh',
  go: 'go',
  cargo: 'rust',
  rustc: 'rust',
  python: 'python',
  pip: 'python',
  poetry: 'python',
  uv: 'python',
  tsc: 'typescript',
  tsx: 'typescript',
  'ts-node': 'typescript',
  vite: 'typescript',
  vitest: 'typescript',
  node: 'node',
  nodejs: 'node',
  npm: 'node',
  npx: 'node',
  yarn: 'node',
  pnpm: 'node',
  bun: 'bun',
  bunx: 'bun',
  docker: 'docker',
  'docker-compose': 'docker',
  podman: 'docker',
  kubectl: 'kubernetes',
  k9s: 'kubernetes',
  helm: 'kubernetes',
  kubectx: 'kubernetes',
  terraform: 'terraform',
  tofu: 'terraform',
  aws: 'aws',
  sam: 'aws',
  psql: 'postgres',
  postgres: 'postgres',
  'redis-cli': 'redis',
  'redis-server': 'redis',
  nginx: 'nginx',
  git: 'git',
  java: 'java',
  javac: 'java',
  mvn: 'java',
  gradle: 'java',
  ruby: 'ruby',
  rails: 'ruby',
  bundle: 'ruby',
  rake: 'ruby',
  php: 'php',
  artisan: 'php',
  bash: 'bash',
  zsh: 'bash',
  fish: 'bash',
  sh: 'bash',
};

// python3, python3.12, ruby3.2, node22: the interpreter under its versioned name.
const VERSIONED_BINARY = /^(python|ruby|php|java|node)[\d.]+$/;

export function detectTerminalFlavor(
  agent: Pick<Worker, 'command'>,
): TerminalFlavor {
  const binary = commandBinary(agent.command).replace(VERSIONED_BINARY, '$1');
  return FLAVOR_BY_BINARY[binary] ?? 'shell';
}

export function terminalFlavorLabel(flavor: TerminalFlavor): string {
  switch (flavor) {
    case 'ssh':
      return 'SSH session';
    case 'go':
      return 'Go';
    case 'rust':
      return 'Rust';
    case 'python':
      return 'Python';
    case 'node':
      return 'Node.js';
    case 'typescript':
      return 'TypeScript';
    case 'bun':
      return 'Bun';
    case 'docker':
      return 'Docker';
    case 'kubernetes':
      return 'Kubernetes';
    case 'postgres':
      return 'PostgreSQL';
    case 'redis':
      return 'Redis';
    case 'nginx':
      return 'Nginx';
    case 'git':
      return 'Git';
    case 'java':
      return 'Java';
    case 'ruby':
      return 'Ruby';
    case 'php':
      return 'PHP';
    case 'terraform':
      return 'Terraform';
    case 'aws':
      return 'AWS';
    case 'bash':
      return 'Shell';
    default:
      return 'Shell terminal';
  }
}
