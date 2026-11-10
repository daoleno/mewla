import { TextView } from "nsh/base";
import type { TextViewDocument } from "nsh/base";

/** Text exactly as written (no Markdown read into it): paragraphs at blank lines, line breaks kept. */
export function plainDocument(text: string): TextViewDocument {
  return {
    blocks: text.split(/\n{2,}/).map((paragraph) => ({
      type: "paragraph" as const,
      content: paragraph.split("\n").flatMap((line, i) => (i === 0 ? [{ type: "text" as const, text: line }] : [{ type: "break" as const }, { type: "text" as const, text: line }])),
    })),
  };
}

/**
 * Selectable plain text. NSH's `Text` cannot be selected (no `selectable`), so
 * what a person can copy is a `TextView` of a plain document.
 */
export function PlainText(props: { text: string; class?: string }) {
  return <TextView document={plainDocument(props.text)} selectable class={props.class} />;
}
