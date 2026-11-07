import remend, { type RemendOptions } from "remend";
import {
  isTimelineProjectionPerfEnabled,
  recordMarkdownPrepareSample,
} from "./timelineProjectionPerf";

/** Shared Android+iOS product path. Fallback is error-boundary only, not a platform fork. */
export const INTERFACE_MARKDOWN_MOBILE_RENDERER = "enriched" as const;

const STREAMING_REMEND_OPTIONS: RemendOptions = {
  images: false,
  inlineKatex: false,
  linkMode: "text-only",
};

/**
 * CommonMark reads a line that is only `-` or `=` under a paragraph as a
 * setext heading, and a bare `-`, `1.` or `>` as an empty list item or quote.
 * While a reply streams, the line `- item` first arrives as `-`, so the
 * paragraph above it would flash as a heading. The half-typed last line waits
 * until it has more.
 */
export function holdBackHalfTypedMarker(source: string): string {
  const body = source.replace(/[ \t\n]+$/, "");
  const lineStart = body.lastIndexOf("\n") + 1;
  const line = body.slice(lineStart);
  if (
    /^[ \t]*(?:-+|=+|[*+]|\d{1,9}[.)]|>[ \t>]*)$/.test(line) &&
    !endsInsideOpenFence(body.slice(0, lineStart))
  ) {
    return source.slice(0, lineStart);
  }
  return source;
}

/** Code is shown as typed, so a lone `-` inside an open fence is a code line, not a marker. */
function endsInsideOpenFence(text: string): boolean {
  let fence: string | null = null;
  for (const line of text.split("\n")) {
    const match = /^ {0,3}(`{3,}|~{3,})/.exec(line);
    if (!match) {
      continue;
    }
    if (!fence) {
      fence = match[1];
    } else if (
      match[1][0] === fence[0] &&
      match[1].length >= fence.length &&
      /^ {0,3}(`{3,}|~{3,})\s*$/.test(line)
    ) {
      fence = null;
    }
  }
  return fence !== null;
}

/** Closes the inline syntax a streaming reply has opened but not yet closed. */
export function remendStreamingMarkdown(markdown: string) {
  return remend(markdown, STREAMING_REMEND_OPTIONS);
}

export function prepareInterfaceMarkdown(value: string, streaming: boolean) {
  const measure = isTimelineProjectionPerfEnabled();
  const started = measure ? nowMs() : 0;
  let markdown = value
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\r\n/g, "\n")
    .trim();
  if (!markdown) {
    if (measure) {
      recordMarkdownPrepareSample({
        durationMs: nowMs() - started,
        streaming,
        inputChars: value.length,
        outputChars: 0,
      });
    }
    return "";
  }
  if (streaming) {
    markdown = remendStreamingMarkdown(holdBackHalfTypedMarker(markdown));
  }
  const prepared = markdown;
  if (measure) {
    recordMarkdownPrepareSample({
      durationMs: nowMs() - started,
      streaming,
      inputChars: value.length,
      outputChars: prepared.length,
    });
  }
  return prepared;
}

function nowMs() {
  const perf = (
    globalThis as { performance?: { now(): number } }
  ).performance;
  return perf?.now?.() ?? Date.now();
}
