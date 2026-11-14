#!/usr/bin/env bash
# Prints the private LAN address (10/8, 172.16/12, 192.168/16) a phone on this network reaches this host at,
# and on stderr which interface it came from. Not the route to the internet: a proxy's or VPN's tunnel
# (mihomo's TUN, Tailscale, WireGuard) can carry that, and a phone cannot reach it. Tunnels, bridges and other
# virtual interfaces (Docker, libvirt, VMs) are skipped; of what is left the interface of a default route
# wins, then the first. Exits 1, saying what it saw, when there is none.
set -euo pipefail

# Interfaces a phone never reaches this host on. On Linux any interface under /sys/devices/virtual/net is
# skipped as well, whatever its name (a TUN named after its proxy).
skip_name='^(lo|docker|br-|virbr|veth|vnet|vmnet|vboxnet|tun|tap|utun|wg|tailscale|zt|cni|flannel|cali|kube|lxc|lxdbr|podman|bridge|awdl|llw|ipsec|ppp|gif|stf|anpi)'
virtual_net="${MEWLA_VIRTUAL_NET_DIR:-/sys/devices/virtual/net}"

private() {
  case "$1" in
    10.*|192.168.*) return 0 ;;
    172.*) local second="${1#172.}"; second="${second%%.*}"; [ "$second" -ge 16 ] && [ "$second" -le 31 ] ;;
    *) return 1 ;;
  esac
}

# "<interface> <address>" per IPv4 address, and the interfaces of default routes.
seen=()
defaults=()
if command -v ip >/dev/null; then
  while read -r _ name _ cidr _; do
    seen+=("${name%:} ${cidr%%/*}")
  done < <(ip -4 -o addr show up scope global)
  while read -r line; do
    set -- $line
    while [ $# -gt 1 ]; do [ "$1" = dev ] && defaults+=("$2"); shift; done
  done < <(ip -4 route show default)
elif command -v ipconfig >/dev/null; then
  for name in $(ifconfig -l); do
    address="$(ipconfig getifaddr "$name" 2>/dev/null || true)"
    [ -n "$address" ] && seen+=("$name $address")
  done
  default="$(route -n get default 2>/dev/null | awk '$1 == "interface:" { print $2 }' || true)"
  [ -n "$default" ] && defaults+=("$default")
fi

candidates=()
for entry in ${seen[@]+"${seen[@]}"}; do
  name="${entry% *}"
  address="${entry#* }"
  [[ "$name" =~ $skip_name ]] && continue
  [ -e "$virtual_net/$name" ] && continue
  private "$address" || continue
  candidates+=("$entry")
done

if [ ${#candidates[@]} -eq 0 ]; then
  {
    echo "No private LAN address found for the phone to reach this host at."
    for entry in ${seen[@]+"${seen[@]}"}; do echo "  skipped ${entry% *} ${entry#* } (a tunnel, a virtual interface or not a LAN address)"; done
    echo "Set DEV_HOST to the address the phone reaches this host at."
  } >&2
  exit 1
fi

chosen="${candidates[0]}"
for entry in "${candidates[@]}"; do
  for name in ${defaults[@]+"${defaults[@]}"}; do
    if [ "${entry% *}" = "$name" ]; then chosen="$entry"; break 2; fi
  done
done

echo "LAN address: ${chosen#* } (${chosen% *})" >&2
for entry in "${candidates[@]}"; do
  [ "$entry" = "$chosen" ] || echo "  also ${entry#* } (${entry% *}); set DEV_HOST to use it" >&2
done
echo "${chosen#* }"
