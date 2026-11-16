import type { JSX } from "nsh";
import { isMermaidLanguage } from "../../core/mermaid/mermaid";
import { MermaidBlock, plainCode } from "./MermaidBlock";

/** How a previewed file's Markdown draws its code blocks: as Markdown does, a `mermaid` one as its diagram. */
export const renderFileCode = (block: { code: string; language?: string }): JSX.Element => {
  if (isMermaidLanguage(block.language)) return <MermaidBlock code={block.code} />;
  return plainCode(block.code, block.language ?? "");
};
