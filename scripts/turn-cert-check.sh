#!/bin/sh
# Checks the certificate TURN serves on turns: (turn.connect.mewla.ai:443, the
# relay's SNI route to the turn accessory), as a Trigger check:
#   - prints nothing while it verifies and has at least DAYS days left;
#   - prints one line that stays the same for one certificate when it has
#     fewer than DAYS days left (renewal is not working), or does not verify;
#   - exits 1 with the reason on stderr when it cannot read a certificate at
#     all (the Trigger reports a check that keeps failing).
#
#   scripts/turn-cert-check.sh [host] [port] [days]
set -u
host=${1:-turn.connect.mewla.ai}
port=${2:-443}
days=${3:-21}

served=$(openssl s_client -connect "$host:$port" -servername "$host" </dev/null 2>/dev/null |
  openssl x509 2>/dev/null)
if [ -z "$served" ]; then
  echo "turn-cert-check: no certificate from $host:$port" >&2
  exit 1
fi
not_after=$(printf '%s\n' "$served" | openssl x509 -noout -enddate | cut -d= -f2)

if ! printf '%s\n' "$served" | openssl x509 -noout -checkend $((days * 86400)) >/dev/null; then
  echo "$host:$port serves a TURN certificate that expires $not_after, fewer than $days days away: its renewal is not working (see the turn accessory's log and /healthz)"
  exit 0
fi

# The handshake's status, not "Verify return code", which openssl prints as 0
# (ok) even after a verify error aborted the handshake.
result=$(openssl s_client -connect "$host:$port" -servername "$host" -verify_hostname "$host" \
  -verify_return_error </dev/null 2>&1)
status=$?
reason=$(printf '%s\n' "$result" | sed -n 's/^verify error:num=[0-9]*://p' | head -n 1)
if [ "$status" -ne 0 ] || [ -n "$reason" ]; then
  echo "$host:$port serves a TURN certificate (expires $not_after) that does not verify: ${reason:-handshake failed}"
fi
exit 0
