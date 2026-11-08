#!/usr/bin/env python3
"""Recording reverse proxy for the WebArena-Verified runs of the agent-use benchmark.

WebArena-Verified judges a task that changes something (or navigates) from the network trace of the run, not from
what the agent says. So that every tool layer is judged the same way, whatever browser it drives, each site is
published on the port the agent's browser opens (the site's own base URL) by this proxy, and the site itself sits
on a private port behind it. The proxy writes a HAR of everything that passes: the same bytes whichever of
mewla_chrome, the virtual-screen browser, agent-browser or browser-use sent them. Standard library only.

  proxy.py --site shopping_admin:7780:17780[:user:pass] --site gitlab:8023:18023 ... --ctl 17999

  --site NAME:PUBLIC:PRIVATE   listen on 127.0.0.1:PUBLIC, forward to 127.0.0.1:PRIVATE with the Host header unchanged
                               (the site was told http://localhost:PUBLIC is its address, so it never redirects around us)
  shopping_admin:...:user:pass adds Magento's auto-login header, as the WebArena-Verified runner does
  --ctl PORT                   control on 127.0.0.1:PORT (never published to the agent): POST /clear, GET /har
                               (the HAR, including each response's Set-Cookie as HAR cookies), GET /health

Static files are in the HAR too (the evaluator ignores them). Responses are buffered, not streamed; a WebSocket
upgrade is refused (no task here depends on one).
"""
import argparse, http.client, http.server, json, socketserver, threading, time
from http.cookies import SimpleCookie

LOCK = threading.Lock()
ENTRIES = []
HOP = {"connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade", "content-length"}
TEXT_BODY_MAX = 300_000


def make_handler(public, private, login):
    class H(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *a):
            pass

        def handle_any(self):
            if self.headers.get("Upgrade", "").lower() == "websocket":
                self.send_response(400); self.send_header("Content-Length", "0"); self.end_headers(); return
            n = int(self.headers.get("Content-Length") or 0)
            body = self.rfile.read(n) if n else b""
            hdrs = [(k, v) for k, v in self.headers.items() if k.lower() not in HOP and k.lower() != "accept-encoding"]
            if login:
                hdrs.append(("X-M2-Admin-Auto-Login", login))
            started = time.time()
            conn = http.client.HTTPConnection("127.0.0.1", private, timeout=120)
            try:
                conn.putrequest(self.command, self.path, skip_host=True, skip_accept_encoding=True)
                for k, v in hdrs:
                    conn.putheader(k, v)
                conn.putheader("Accept-Encoding", "identity")
                conn.putheader("Content-Length", str(len(body)))
                conn.endheaders(body)
                r = conn.getresponse()
                data = r.read()
            except Exception as e:  # the site is down or slow: say so, record nothing
                msg = ("proxy: " + str(e)).encode()
                self.send_response(502); self.send_header("Content-Length", str(len(msg))); self.end_headers(); self.wfile.write(msg); return
            finally:
                conn.close()
            rh = [(k, v) for k, v in r.getheaders() if k.lower() not in HOP]
            self.send_response(r.status, r.reason)
            for k, v in rh:
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(data)
            record(self, public, hdrs, body, r, rh, data, started)

        do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = do_HEAD = do_OPTIONS = handle_any
    return H


def record(h, public, req_headers, body, r, rh, data, started):
    host = h.headers.get("Host") or "localhost:%d" % public
    url = "http://%s%s" % (host, h.path)
    # The browser's own headers only (not the one the proxy adds): what the evaluator reads is what the browser sent.
    ctype = h.headers.get("Content-Type", "")
    req = {"method": h.command, "url": url, "httpVersion": "HTTP/1.1", "headers": [{"name": k, "value": v} for k, v in h.headers.items()],
           "queryString": [], "headersSize": -1, "bodySize": len(body)}
    if body:
        req["postData"] = {"mimeType": ctype, "text": body.decode("utf-8", "replace")}
    cookies = []
    for k, v in rh:
        if k.lower() == "set-cookie":
            c = SimpleCookie()
            try:
                c.load(v)
            except Exception:
                continue
            cookies += [{"name": m.key, "value": m.value} for m in c.values()]
    mime = r.getheader("Content-Type", "")
    content = {"size": len(data), "mimeType": mime}
    if "json" in mime and len(data) <= TEXT_BODY_MAX:
        content["text"] = data.decode("utf-8", "replace")
    loc = r.getheader("Location", "")
    resp = {"status": r.status, "statusText": r.reason, "httpVersion": "HTTP/1.1", "headers": [{"name": k, "value": v} for k, v in rh],
            "cookies": cookies, "content": content, "redirectURL": loc, "headersSize": -1, "bodySize": len(data)}
    with LOCK:
        ENTRIES.append({"startedDateTime": time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(started)) + "Z", "time": round((time.time() - started) * 1000),
                        "request": req, "response": resp, "cache": {}, "timings": {"send": 0, "wait": 0, "receive": 0}})


class Ctl(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def reply(self, code, body, ctype="application/json"):
        b = body.encode()
        self.send_response(code); self.send_header("Content-Type", ctype); self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)

    def do_GET(self):
        if self.path == "/health":
            return self.reply(200, "ok", "text/plain")
        if self.path == "/har":
            with LOCK:
                return self.reply(200, json.dumps({"log": {"version": "1.2", "creator": {"name": "aub-wav-proxy", "version": "1"}, "entries": list(ENTRIES)}}))
        self.reply(404, "{}")

    def do_POST(self):
        if self.path == "/clear":
            with LOCK:
                ENTRIES.clear()
            return self.reply(200, "{}")
        self.reply(404, "{}")


class Threaded(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True
    request_queue_size = 128


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--site", action="append", required=True)
    ap.add_argument("--ctl", type=int, required=True)
    a = ap.parse_args()
    for s in a.site:
        parts = s.split(":", 3)
        name, public, private = parts[0], int(parts[1]), int(parts[2])
        login = parts[3] if len(parts) > 3 else None
        srv = Threaded(("127.0.0.1", public), make_handler(public, private, login))
        threading.Thread(target=srv.serve_forever, daemon=True).start()
    Threaded(("127.0.0.1", a.ctl), Ctl).serve_forever()


if __name__ == "__main__":
    main()
