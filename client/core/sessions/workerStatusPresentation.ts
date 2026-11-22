/** From the Expo app's services/workerStatusPresentation.ts. */
import type { Worker, WorkerStatus } from './workers';

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

const RELAUNCH_WORD = { restarting: 'Restarting', restarted: 'Restarted', stopped: 'Stopped' } as const;

/** The Session header's state word: its agent's restart while that is the news, else its status. */
export function sessionStateWord(worker: Pick<Worker, 'status' | 'relaunch'>): string {
  return worker.relaunch ? RELAUNCH_WORD[worker.relaunch.state] : workerStatusLabel(worker.status);
}

export function buildWorkerSessionAccessibilityLabel({
  title,
  status,
  preview,
  timeLabel,
  brainDelegated,
  fromCalendar = false,
}: {
  title: string;
  status: WorkerStatus;
  preview: string;
  timeLabel: string;
  brainDelegated: boolean;
  fromCalendar?: boolean;
}): string {
  return [
    title,
    fromCalendar ? 'From Calendar' : null,
    brainDelegated ? 'Brain delegated' : null,
    workerStatusLabel(status),
    preview,
    isWorkerActivelyRunning(status) ? null : timeLabel,
  ]
    .filter((part): part is string => Boolean(part))
    .join(', ');
}
