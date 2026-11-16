// The Cloudflare Worker entry (wrangler.toml main): only the default export, since the Workers runtime
// treats every named export of the main module as an entrypoint. The logic and its tests are in
// receiver.mjs.
import { handle } from "./receiver.mjs";

export default { fetch: (request, env) => handle(request, env) };
