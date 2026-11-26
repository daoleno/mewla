#!/usr/bin/env bash
# A Provider's page shows what the lane daemon kept of its requests: Claude Code is routed through a connection to a
# local fake Anthropic endpoint, one request through the daemon's gateway is answered (with prompt-cache tokens) and
# one is refused. The page shows the request's tokens and the failure's words. Claude goes back to what it
# used before and the connection is removed at the end.
set -euo pipefail
lane="${MEWLA_LANE_HOME:-$HOME/.mewla/lane}"
sock="${MEWLA_CONTROL_SOCK:-$lane/state/run/control.sock}"
tag=$RANDOM
dir="$(mktemp -d "${TMPDIR:-/tmp}/provider-usage.XXXXXX")"
# The fake: Anthropic Messages answers once with usage, then refuses; an empty request is refused as
# Anthropic refuses it (the daemon's API detection), and it lists one model.
cat > "$dir/fake.py" <<'PY'
import http.server, json, sys
asked = 0
class H(http.server.BaseHTTPRequestHandler):
    def reply(self, code, body):
        raw = json.dumps(body).encode()
        self.send_response(code); self.send_header("content-type", "application/json"); self.send_header("content-length", str(len(raw))); self.end_headers(); self.wfile.write(raw)
    def do_GET(self): self.reply(200, {"data": [{"id": "lane-model"}]})
    def do_POST(self):
        global asked
        body = self.rfile.read(int(self.headers.get("content-length") or 0))
        if body.strip() == b"{}":
            return self.reply(400, {"type": "error", "error": {"type": "invalid_request_error", "message": "model: Field required"}})
        asked += 1
        if asked == 1:
            return self.reply(200, {"id": "msg_lane", "type": "message", "role": "assistant", "model": "lane-model", "content": [{"type": "text", "text": "ok"}],
                                    "stop_reason": "end_turn", "usage": {"input_tokens": 1200, "cache_read_input_tokens": 48000, "output_tokens": 350}})
        self.reply(400, {"type": "error", "error": {"type": "invalid_request_error", "message": "Lane fake refused this request"}})
    def log_message(self, *a): pass
s = http.server.HTTPServer(("127.0.0.1", 0), H)
open(sys.argv[1], "w").write(str(s.server_port))
s.serve_forever()
PY
python3 -I "$dir/fake.py" "$dir/port" & fake=$!
ctl() {
  python3 -I -c 'import json,socket,sys
s = socket.socket(socket.AF_UNIX); s.connect(sys.argv[1]); s.settimeout(60)
s.sendall((sys.argv[2] + "\n").encode()); buf = b""
while True:
    chunk = s.recv(65536)
    if not chunk: break
    buf += chunk
    try: print(json.dumps(json.loads(buf))); break
    except ValueError: pass' "$sock" "$1"
}
field() { python3 -I -c 'import json,sys; v = json.load(sys.stdin)
for k in sys.argv[1].split("."): v = v.get(k, "") if isinstance(v, dict) else ""
print(v)' "$1"; }
id_of() { python3 -I -c 'import json,sys; print(next(c["id"] for c in json.load(sys.stdin)["providers"]["connections"] if c["name"] == sys.argv[1]))' "$1"; }
revision() { ctl '{"type":"provider_list"}' | field providers.revision; }
name="Lane usage $tag"
before=$(ctl '{"type":"provider_list"}' | field providers.defaults.claude.connection_id)
cleanup() {
  ctl "{\"type\":\"provider_set_connection\",\"client\":\"claude\",\"connection_id\":\"$before\",\"revision\":$(revision)}" > /dev/null || true
  id=$(ctl '{"type":"provider_list"}' | id_of "$name" 2>/dev/null) &&
    ctl "{\"type\":\"provider_delete\",\"connection_id\":\"$id\",\"revision\":$(revision)}" > /dev/null || true
  kill "$fake" 2>/dev/null || true
}
trap cleanup EXIT
for _ in $(seq 1 20); do [[ -s "$dir/port" ]] && break; sleep 0.2; done
ctl "{\"type\":\"provider_upsert\",\"operation\":\"create\",\"revision\":$(revision),\"credential\":\"lane-fixture-key\",\"provider_connection\":{\"name\":\"$name\",\"client\":\"claude\",\"preset_id\":\"custom\",\"base_url\":\"http://127.0.0.1:$(cat "$dir/port")\",\"model_id\":\"lane-model\",\"advanced\":true}}" > /dev/null
id=$(ctl '{"type":"provider_list"}' | id_of "$name")
ctl "{\"type\":\"provider_set_connection\",\"client\":\"claude\",\"connection_id\":\"$id\",\"revision\":$(revision)}" > /dev/null
gateway=$(ctl '{"type":"provider_list"}' | field providers.gateway.address)
running=$(ctl '{"type":"provider_list"}' | field providers.gateway.running)
# A gateway that could not listen (its saved port taken by another daemon) cannot carry the requests.
[[ "$running" == "True" && -n "$gateway" ]] || { echo "the daemon's gateway is not running (address '${gateway}'); see its log" >&2; exit 1; }
ask() {
  curl -s -o /dev/null -w '%{http_code}\n' -X POST "http://$gateway/v1/messages" -H 'content-type: application/json' \
    -H 'anthropic-version: 2023-06-01' -H 'x-api-key: placeholder' \
    -d '{"model":"lane-model","max_tokens":20,"messages":[{"role":"user","content":"ping"}]}'
}
echo "answered $(ask), failed $(ask) (gateway $gateway)"
ctl "{\"type\":\"provider_usage\",\"connection_id\":\"$id\"}" | python3 -I -c 'import json,sys
u = json.load(sys.stdin)["provider_usage"]
assert len(u["requests"]) == 1 and u["requests"][0]["cache_read_tokens"] == 48000, u
assert len(u["errors"]) == 1 and u["errors"][0]["status"] == 400, u
print("daemon kept", len(u["requests"]), "request,", len(u["errors"]), "error")'

# A row below the fold is not on the screen: the release replay's emulator is 320x640, the lane's 1080x2400.
reach() { for _ in 1 2 3 4 5; do "$PHONE" tap "$1" 3 && return 0; "$PHONE" scroll; done; "$PHONE" tap "$1" 3; }
see() { for _ in 1 2 3 4 5; do "$PHONE" shows "$1" 3 && return 0; "$PHONE" scroll; done; "$PHONE" shows "$1" 3; }
"$PHONE" tap 'home-cat'
reach '^Settings$'
reach '^Model Providers$'
reach "^$name, "  # the Provider's row, not the agent chip that names it too
"$PHONE" shows 'provider-detail' 15
see 'Usage · 30 days'  # shows greps the raw XML: no anchors
"$PHONE" shot "$dir/usage-top.png" > /dev/null
# The page's recent requests and errors are under its tiles.
see '1.2K in · 48K cached · 350 out'
see 'Lane fake refused this request'
"$PHONE" shot "$dir/usage.png" > /dev/null
echo "shots $dir/usage-top.png $dir/usage.png"
