#!/usr/bin/env bash
# Install (or update) the CI deploy entry on the Kamal host, by hand, from a machine that holds
# the host's admin key. CI cannot change any of this: its keys only run bin/deploy.
#
#   scripts/kamal-host/install.sh                      script, service configs, Kamal image, Kamal's own key
#   scripts/kamal-host/install.sh SERVICE PUBKEY_FILE  also authorize a CI deploy key for SERVICE
#
# One SSH connection (the host's ufw allows six new ones per 30 s).
set -euo pipefail

HOST=${KAMAL_HOST:-ubuntu@167.114.116.27}
KEY=${KAMAL_HOST_KEY:-$HOME/.ssh/id_ed25519_lenscan}
KAMAL_IMAGE=ghcr.io/basecamp/kamal@sha256:7b5be276aa17bbe122887f6a1ff12865f4848111989699b9786083be95d98415 # v2.12.0
HERE=$(cd "$(dirname "$0")" && pwd)

service=${1:-} pub=
if [ -n "$service" ]; then
  [ -f "$HERE/$service.conf" ] || { echo "no $service.conf" >&2; exit 64; }
  pub=$(cat "${2:?public key file}")
  [[ "$pub" =~ ^ssh-ed25519\ [A-Za-z0-9+/=]+(\ [A-Za-z0-9@._-]+)?$ ]] || { echo "not one ssh-ed25519 public key" >&2; exit 64; }
fi

(cd "$HERE" && tar -cf - deploy ./*.conf) |
  ssh -i "$KEY" -o IdentitiesOnly=yes "$HOST" \
    "KAMAL_IMAGE='$KAMAL_IMAGE' SERVICE='$service' PUB='$pub' bash -c '$(cat <<'REMOTE'
set -euo pipefail
umask 077
D=/home/ubuntu/deploy
mkdir -p "$D"/{bin,etc,state,work,log,kamal-ssh}
inbox=$(mktemp -d)
tar -C "$inbox" -xf -
install -m 0755 "$inbox/deploy" "$D/bin/deploy"
for f in "$inbox"/*.conf; do install -m 0644 "$f" "$D/etc/"; done
rm -rf "$inbox"

docker image inspect "$KAMAL_IMAGE" >/dev/null 2>&1 || docker pull -q "$KAMAL_IMAGE" >/dev/null
echo "$KAMAL_IMAGE" >"$D/etc/kamal-image"
printf "root:x:0:0::/root:/bin/sh\nci-deploy:x:1000:1000::/kamal-home:/bin/sh\n" >"$D/etc/passwd"
chmod 0644 "$D/etc/passwd" "$D/etc/kamal-image"

# Kamal on this host reaches the host over SSH like any Kamal: with a key of its own that works only
# from the host itself (Kamal connects to the public address, which arrives on lo). The file name
# is the one config/deploy*.yml name in ssh.keys, so their ~/.ssh path resolves in the container.
k="$D/kamal-ssh/id_ed25519_lenscan"
[ -f "$k" ] || ssh-keygen -q -t ed25519 -N "" -C kamal-on-host -f "$k"
chmod 0600 "$k"
for h in /etc/ssh/ssh_host_*_key.pub; do echo "167.114.116.27 $(cut -d" " -f1,2 "$h")"; done >"$D/kamal-ssh/known_hosts"
chmod 0644 "$D/kamal-ssh/known_hosts" "$k.pub"

ak=/home/ubuntu/.ssh/authorized_keys
cp -p "$ak" "$D/state/authorized_keys.before-$(date -u +%Y%m%d%H%M%S)"
line="from=\"127.0.0.1,::1,167.114.116.27\",no-agent-forwarding,no-port-forwarding,no-X11-forwarding,no-pty $(cut -d" " -f1,2 "$k.pub") kamal-on-host"
grep -qF "$(cut -d" " -f2 "$k.pub")" "$ak" || echo "$line" >>"$ak"
if [ -n "$SERVICE" ]; then
  keyb=$(echo "$PUB" | cut -d" " -f2)
  grep -qF "$keyb" "$ak" || echo "restrict,command=\"$D/bin/deploy $SERVICE\" $(echo "$PUB" | cut -d" " -f1,2) ci-deploy-$SERVICE" >>"$ak"
fi
ls -1 "$D/state"/authorized_keys.before-* | head -n -5 | xargs -r rm -f
echo "installed: $(sha256sum "$D/bin/deploy" | cut -c1-12) bin/deploy; services: $(cd "$D/etc" && ls *.conf | tr "\n" " ")"
echo "authorized_keys: $(wc -l <"$ak") keys ($(grep -c "command=\"$D/bin/deploy" "$ak" || true) CI deploy keys)"
REMOTE
)'"
