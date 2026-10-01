/** Run from the repository root with Expo Metro on 8081. Loopback-only fixture shell. */
const metro = "http://127.0.0.1:8081";
const server = Bun.serve({
  hostname: "127.0.0.1",
  port: 8097,
  async fetch(request) {
    const url = new URL(request.url);
    if (url.pathname !== "/") {
      const response = await fetch(`${metro}${url.pathname}${url.search}`, { headers: { "Accept-Encoding": "identity" } });
      return new Response(await response.arrayBuffer(), { status: response.status, headers: { "Content-Type": response.headers.get("Content-Type") ?? "application/octet-stream" } });
    }
    return new Response(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Resources · synthetic fixture</title><style>html,body,#root{height:100%;margin:0}#root{display:flex}body{overflow:hidden}</style></head><body><div id="root"></div><script src="/app/__fixtures__/telemetry/dashboard-preview.bundle?platform=web&dev=true&hot=false"></script></body></html>`, {
      headers: { "Content-Type": "text/html" },
    });
  },
});
console.log(`Resources fixture: ${server.url}`);
