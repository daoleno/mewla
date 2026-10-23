// The in-band attachment envelope: the phone app (and Telegram, via
// daemon/attachment) appends it to a message so the Worker can read the
// uploaded files' paths. This is its one reader and writer in the app; the
// Go counterpart is daemon/attachment/input.go.

export type EnvelopeAttachment = {
  name: string;
  path: string;
  mimeType?: string;
};

const ENVELOPE_RE = /<mewla_attachments>\s*([\s\S]*?)\s*<\/mewla_attachments>/i;

export function buildAttachmentEnvelope(
  attachments: ReadonlyArray<EnvelopeAttachment>,
): string {
  return `<mewla_attachments>${JSON.stringify({
    files: attachments.map((attachment) => ({
      name: attachment.name,
      path: attachment.path,
      content_type: attachment.mimeType,
    })),
  })}</mewla_attachments>`;
}

// The text without its envelope, and the files the envelope listed. Text
// without an envelope, or with an unreadable one, lists none.
export function splitAttachmentEnvelope(text: string): {
  text: string;
  attachments: EnvelopeAttachment[];
} {
  const match = ENVELOPE_RE.exec(text);
  if (!match) {
    return { text, attachments: [] };
  }
  return {
    text: text.replace(match[0], ""),
    attachments: parseEnvelopeFiles(match[1]),
  };
}

export function stripAttachmentEnvelope(text: string): string {
  return text.replace(ENVELOPE_RE, "");
}

function parseEnvelopeFiles(value: string): EnvelopeAttachment[] {
  try {
    const parsed = JSON.parse(value.trim());
    const files = Array.isArray(parsed?.files) ? parsed.files : [];
    return files
      .map((file: unknown) => {
        const entry = (file ?? {}) as Record<string, unknown>;
        return {
          name: typeof entry.name === "string" ? entry.name.trim() : "",
          path: typeof entry.path === "string" ? entry.path.trim() : "",
          mimeType:
            typeof entry.content_type === "string"
              ? entry.content_type
              : undefined,
        };
      })
      .filter((file: EnvelopeAttachment) => file.path);
  } catch {
    return [];
  }
}
