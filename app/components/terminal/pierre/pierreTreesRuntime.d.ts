import type { TreeHostMessage } from "./treeViewerProtocol";

export function mountTreeViewer(
  container: HTMLElement,
  post: (message: unknown) => void,
): { handle(batch: TreeHostMessage[]): void; destroy(): void };
