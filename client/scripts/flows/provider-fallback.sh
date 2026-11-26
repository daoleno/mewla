#!/usr/bin/env bash
# A Provider's page picks its Fallback against the lane daemon: two Claude connections, one at an address where
# nothing listens (it speaks Anthropic, Claude Code's own API) and one in front of a Chat-only endpoint (a local fake
# that answers the daemon's detection as Chat Completions does). The page offers the Chat one, the pick reaches the
# daemon, and the chip shows it. Both connections are removed at the end.
set -euo pipefail
lane="${MEWLA_LANE_HOME:-$HOME/.mewla/lane}"
sock="$lane/state/run/control.sock"
tag=$RANDOM
dir="$(mktemp -d "${TMPDIR:-/tmp}/provider-fallback.XXXXXX")"
# The fake: 404 for Anthropic Messages, 400 for an empty Chat Completions request, an empty model list.
cat > "$dir/fake.py" <<'PY'
import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    def reply(self, code, body):
        self.send_response(code); self.send_header("content-type", "application/json"); self.end_headers(); self.wfile.write(body)
    def do_GET(self): self.reply(200, b'{"data":[]}')
    def do_POST(self):
        self.rfile.read(int(self.headers.get("content-length") or 0))
        self.reply(400 if self.path.endswith("/chat/completions") else 404, b'{"error":{"message":"fake"}}')
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
id_of() { python3 -I -c 'import json,sys; print(next(c["id"] for c in json.load(sys.stdin)["providers"]["connections"] if c["name"] == sys.argv[1]))' "$1"; }
add() {
  local rev
  rev=$(ctl '{"type":"provider_list"}' | python3 -I -c 'import json,sys; print(json.load(sys.stdin)["providers"]["revision"])')
  ctl "{\"type\":\"provider_upsert\",\"operation\":\"create\",\"revision\":$rev,\"credential\":\"lane-fixture-key\",\"provider_connection\":{\"name\":\"$1\",\"client\":\"claude\",\"preset_id\":\"custom\",\"base_url\":\"$2\",\"advanced\":true}}" > /dev/null
}
main="Lane main $tag" chat="Lane chat $tag"
cleanup() {
  for name in "$main" "$chat"; do
    id=$(ctl '{"type":"provider_list"}' | id_of "$name" 2>/dev/null) || continue
    rev=$(ctl '{"type":"provider_list"}' | python3 -I -c 'import json,sys; print(json.load(sys.stdin)["providers"]["revision"])')
    ctl "{\"type\":\"provider_delete\",\"connection_id\":\"$id\",\"revision\":$rev}" > /dev/null || true
  done
  kill "$fake" 2>/dev/null || true
}
trap cleanup EXIT
for _ in $(seq 1 20); do [[ -s "$dir/port" ]] && break; sleep 0.2; done
add "$main" "http://127.0.0.1:9"
add "$chat" "http://127.0.0.1:$(cat "$dir/port")/v1"
ctl '{"type":"provider_list"}' | python3 -I -c 'import json,sys
c = {x["name"]: x for x in json.load(sys.stdin)["providers"]["connections"]}
print("chat speaks", c[sys.argv[1]].get("upstream_api"))' "$chat"

"$PHONE" tap 'home-cat'
"$PHONE" tap '^Settings$'
"$PHONE" tap '^Model Providers$'
"$PHONE" tap "$main"
"$PHONE" shows 'Anthropic Messages'  # shows greps the raw XML: no anchors
"$PHONE" tap 'Fallback None'
"$PHONE" tap "$chat"
"$PHONE" shows "Fallback $chat"
"$PHONE" shot "$dir/fallback.png" > /dev/null
ctl '{"type":"provider_list"}' | python3 -I -c 'import json,sys
c = {x["name"]: x for x in json.load(sys.stdin)["providers"]["connections"]}
fb = c[sys.argv[1]].get("fallback_connection_id")
assert fb == c[sys.argv[2]]["id"], fb
print("daemon fallback", fb)' "$main" "$chat"
echo "shot $dir/fallback.png"
