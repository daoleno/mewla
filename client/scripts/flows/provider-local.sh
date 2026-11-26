#!/usr/bin/env bash
# Add Provider offers a model server running on the computer (the lane daemon's, so this host's) and adds it with no
# key; the daemon finds which API it speaks. Needs one: Ollama on 11434 (any version), else an OpenAI-style server
# on 1234, 8000, 8080 or 1337. The connection is removed at the end.
set -euo pipefail
lane="${MEWLA_LANE_HOME:-$HOME/.mewla/lane}"
# The release replay has no lane: its daemon's socket comes in MEWLA_CONTROL_SOCK (release-flows.sh).
sock="${MEWLA_CONTROL_SOCK:-$lane/state/run/control.sock}"
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
before=$(ctl '{"type":"provider_list"}' | python3 -I -c 'import json,sys; print(" ".join(c["id"] for c in json.load(sys.stdin)["providers"]["connections"]))')
server=$(ctl '{"type":"provider_detect_local"}' | python3 -I -c 'import json,sys; f=json.load(sys.stdin).get("local_providers") or []; print(f[0]["label"] if f else "")')
[[ -n "$server" ]] || { echo "no model server runs on this computer"; exit 1; }
echo "found $server"
cleanup() {
  ctl '{"type":"provider_list"}' | python3 -I -c 'import json,sys
before = set(sys.argv[1].split())
d = json.load(sys.stdin)["providers"]
print("\n".join(c["id"] for c in d["connections"] if c["id"] not in before))' "$before" | while read -r id; do
    [[ -n "$id" ]] || continue
    rev=$(ctl '{"type":"provider_list"}' | python3 -I -c 'import json,sys; print(json.load(sys.stdin)["providers"]["revision"])')
    ctl "{\"type\":\"provider_delete\",\"connection_id\":\"$id\",\"revision\":$rev}" > /dev/null || true
  done
}
trap cleanup EXIT

"$PHONE" tap 'home-cat'
"$PHONE" tap '^Settings$'
"$PHONE" tap '^Model Providers$'
"$PHONE" tap '^Add Provider$'
"$PHONE" shows 'On this computer'   # shows greps the raw XML: no anchors
"$PHONE" tap "^$server\$"
"$PHONE" shows 'provider-url'
! "$PHONE" shows 'provider-key' 1
"$PHONE" tap '^Add Provider$'
"$PHONE" shows 'provider-detail' 30
dir="$(mktemp -d "${TMPDIR:-/tmp}/provider-local.XXXXXX")"
"$PHONE" shot "$dir/local.png" > /dev/null
ctl '{"type":"provider_list"}' | python3 -I -c 'import json,sys
before = set(sys.argv[1].split())
new = [c for c in json.load(sys.stdin)["providers"]["connections"] if c["id"] not in before]
assert len(new) == 1 and new[0]["credential_ready"] and new[0].get("upstream_api"), new
print("added", new[0]["name"], new[0]["base_url"], new[0]["upstream_api"], "keyless")' "$before"
echo "shot $dir/local.png"
