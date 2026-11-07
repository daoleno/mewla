import type { DiffHostMessage } from "./diffViewerProtocol";

export function mountDiffViewer(
  container: HTMLElement,
  post: (message: unknown) => void,
): { handle(batch: DiffHostMessage[]): void; destroy(): void };
