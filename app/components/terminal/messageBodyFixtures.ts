/** One chat message that touches every Markdown element the message body renders. */
export const MESSAGE_KITCHEN_SINK = `# Release notes

A paragraph with **bold**, *italic*, ~~strike~~, \`inline code\`, a [link](https://example.com), a bare https://example.com/a_b link and an entity &amp; &copy; 2026.
It wraps onto a second source line.

## Quote and list

> Heng, can we reuse the Lima runners?
> 1. Are they org-level or repo-level?
> 2. Is the VM Linux?
>
> > nested quote
> > - with a list

- first
- second with a [link](https://example.com)
  - nested a
  - nested b
    1. deep one
    2. deep two
- [x] done task
- [ ] open task

3. starts at three
4. four

## Table

| Name | Count | Note |
| :--- | ---: | :---: |
| alpha | 1 | \`ok\` |
| beta | 22 | **bold** |
| gamma | 333 | a \\| pipe |

## Code and diagram

\`\`\`ts
export function add(a: number, b: number) {
  return a + b;
}
\`\`\`

\`\`\`mermaid
flowchart LR
  A[Phone] --> B[Daemon] --> C[Worker]
\`\`\`

---

Last paragraph after a rule.`;

/** A long agent reply, `repeat` copies of a mixed section. */
export function longMessage(sections: number): string {
  const section = `## Section

Paragraph with **bold**, \`code\`, and a [link](https://example.com/x) that wraps
across two source lines.

- item one
- item two with \`code\`
  - nested item
- item three

> a quoted note
> with a second line

| a | b | c |
| --- | --- | --- |
| 1 | 2 | 3 |
| 4 | 5 | 6 |

\`\`\`ts
const value = 1;
console.log(value);
\`\`\`

`;
  return Array.from({ length: sections }, () => section).join("\n");
}
