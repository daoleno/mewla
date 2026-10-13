import type { ScreenHostMessage } from "./screenViewerProtocol";

export function mountScreenViewer(
  container: HTMLElement,
  post: (message: unknown) => void,
): { handle(batch: ScreenHostMessage[]): void; destroy(): void };
