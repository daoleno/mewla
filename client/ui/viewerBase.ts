import { platform } from "nsh";

/**
 * The base URL of Mewla's own viewer documents (the Screen viewer, an SVG preview): `.invalid`
 * never resolves and their CSP forbids every request; on a phone it gives the document a secure
 * context (WebCodecs needs one). In a browser the document is a srcdoc iframe of the page itself,
 * already in the page's context, and the page's CSP (`base-uri 'self'`) refuses that base, so
 * none is set there.
 */
export const viewerBaseUrl: string | undefined = platform.os === "web" ? undefined : "https://viewer.mewla.invalid/";
