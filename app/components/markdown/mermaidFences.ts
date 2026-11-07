export function isMermaidFenceLanguage(language: string | undefined) {
  const value = language?.trim().toLowerCase();
  return value === "mermaid";
}
