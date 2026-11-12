/**
 * A draft with text written from elsewhere added (Work › Ask Brain about this, SPEC §3.4): below what
 * the person had typed, after a blank line, so nothing they wrote is lost; the text alone in an empty
 * draft. Text the draft already ends with is not added twice.
 */
export function appendToDraft(draft: string, text: string): string {
  const typed = draft.replace(/\s+$/u, "");
  if (!typed) return text;
  if (!text.trim() || typed.endsWith(text.replace(/\s+$/u, ""))) return draft;
  return `${typed}\n\n${text}`;
}
