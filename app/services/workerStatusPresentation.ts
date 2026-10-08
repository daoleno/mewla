import type { WorkerStatus } from '../constants/tokens';
import type { IconName } from '../components/icons/Icon';

export type WorkerStatusIndicatorIcon = Extract<
  IconName,
  'check-circle-fill' | 'close-circle-fill' | 'pause-circle' | 'help'
>;

/** True only for durable active-turn Running from the daemon contract. */
export function isWorkerActivelyRunning(status: WorkerStatus): boolean {
  return status === 'running';
}

export function workerStatusLabel(status: WorkerStatus): string {
  switch (status) {
    case 'failed':
      return 'Failed';
    case 'blocked':
      return 'Blocked';
    case 'running':
      return 'Running';
    case 'done':
      return 'Done';
    case 'unknown':
      // Backend unknown = no durable activity signal; list rows are live panes.
      return 'Idle';
    default:
      return 'Idle';
  }
}

export function workerStatusIndicatorIcon(
  status: WorkerStatus,
): WorkerStatusIndicatorIcon | null {
  switch (status) {
    case 'running':
      return null;
    case 'done':
      return 'check-circle-fill';
    case 'failed':
      return 'close-circle-fill';
    case 'blocked':
      return 'pause-circle';
    case 'unknown':
      return 'help';
  }
}

export function buildWorkerSessionAccessibilityLabel({
  title,
  status,
  preview,
  timeLabel,
  brainDelegated,
}: {
  title: string;
  status: WorkerStatus;
  preview: string;
  timeLabel: string;
  brainDelegated: boolean;
}): string {
  return [
    title,
    brainDelegated ? 'Brain delegated' : null,
    workerStatusLabel(status),
    preview,
    isWorkerActivelyRunning(status) ? null : timeLabel,
  ]
    .filter((part): part is string => Boolean(part))
    .join(', ');
}
