/**
 * The radius of the spinner's arc in a canvas of this size (a 2 pt stroke stays inside), or null
 * when there is no room: a canvas drawn before layout, or hidden, is 0×0, and an arc with a
 * negative radius throws (a browser's canvas: `IndexSizeError`, FAILURES R4 #11).
 */
export function spinnerRadius(width: number, height: number): number | null {
  const radius = Math.min(width, height) / 2 - 1.5;
  return radius > 0 ? radius : null;
}
