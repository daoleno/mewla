/**
 * A failed answer in words. A proxy in the way (tunnel, dev server) answers
 * with an HTML page, which is never shown.
 */
export function screenFailureText(status: number, body: string): string {
  const text = body.trim();
  if (text && !text.startsWith("<")) return text.slice(0, 300);
  return status >= 502 && status <= 504 ? "Can't reach this computer." : "Screen request failed";
}
