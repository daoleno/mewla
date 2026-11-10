import type { Frame, Transport } from "../transport";

/** A file staged in the upload store, sent next to the text as a typed field. */
export type SendAttachment = {
  name: string;
  path: string;
  content_type?: string;
};

export type StructuredCommandFailure = {
  requestId: string;
  code: string;
  message: string;
};

export type StructuredCommandOutcome =
  | { kind: "sent" }
  | { kind: "pending"; code?: string; message?: string }
  | { kind: "failed"; failure: StructuredCommandFailure }
  | { kind: "connection_closed" };

export type StructuredCommandReceipt = {
  requestId: string;
  outcome: Promise<StructuredCommandOutcome>;
};

export interface SendInput {
  requestId: string;
  workerId: string;
  text: string;
  attachments?: SendAttachment[];
  conversationScopeKey?: string;
}

/**
 * Sends one `send_input` and reports only what the daemon said about it: sent,
 * pending (`input_uncertain` when it can't tell), failed, or the connection
 * closed first. Never throws: a send while offline is `connection_closed`, and
 * the outbox sends it again on the next connection under the same id.
 */
export function sendInput(transport: Transport, input: SendInput): StructuredCommandReceipt {
  const { requestId } = input;
  const outcome = new Promise<StructuredCommandOutcome>((resolve) => {
    const stops: Array<() => void> = [];
    const settle = (value: StructuredCommandOutcome) => {
      stops.forEach((stop) => stop());
      resolve(value);
    };
    const mine = (frame: Frame) => frame.request_id === requestId;
    stops.push(transport.on("input_sent", (frame) => mine(frame) && settle({ kind: "sent" })));
    stops.push(
      transport.on("input_pending", (frame) =>
        mine(frame) && settle({ kind: "pending", code: optional(frame.code), message: optional(frame.message) })),
    );
    stops.push(
      transport.on("input_failed", (frame) =>
        mine(frame) &&
        settle({ kind: "failed", failure: { requestId, code: String(frame.code ?? "input_failed"), message: String(frame.message ?? "") } })),
    );
    stops.push(transport.on("disconnected", () => settle({ kind: "connection_closed" })));
    try {
      transport.send({
        type: "send_input",
        request_id: requestId,
        worker_id: input.workerId,
        text: input.text,
        ...(input.attachments?.length ? { attachments: input.attachments } : {}),
        ...(input.conversationScopeKey ? { conversation_scope_key: input.conversationScopeKey } : {}),
      });
    } catch {
      settle({ kind: "connection_closed" });
    }
  });
  return { requestId, outcome };
}

function optional(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined;
}
