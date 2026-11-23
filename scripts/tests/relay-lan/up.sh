#!/usr/bin/env bash
# up.sh E: a Mewla Relay of its own, all on 127.0.0.1 (macOS or Linux): pebble and its DNS (challtestsrv, which
# answers every name with 127.0.0.1), the account service with a fake sign-in, and mewla-relay. Binaries in $E/bin.
# Hosted names are <label>.mewla.test on port 19443.
set -euo pipefail
E="$1"; cd "$E"
mkdir -p certs logs pids stubs
printf '#!/bin/sh\nexit 1\n' > stubs/systemd-run; chmod +x stubs/systemd-run
bg() { local name=$1; shift; if command -v setsid >/dev/null; then setsid "$@" > "logs/$name.log" 2>&1 < /dev/null & else nohup "$@" > "logs/$name.log" 2>&1 < /dev/null & fi; echo $! > "pids/$name"; }
if [[ ! -f certs/ca.crt ]]; then
  openssl ecparam -name prime256v1 -genkey -noout -out certs/ca.key
  openssl req -x509 -new -key certs/ca.key -days 3 -subj /CN=relay-lan-ca -out certs/ca.crt
  for n in pebble relay; do
    if [[ $n == pebble ]]; then san="IP:127.0.0.1,DNS:localhost"; else san="DNS:relay.mewla.test,IP:127.0.0.1"; fi
    printf 'subjectAltName=%s\n' "$san" > certs/$n.ext
    openssl ecparam -name prime256v1 -genkey -noout -out certs/$n.key
    openssl req -new -key certs/$n.key -subj /CN=$n -out certs/$n.csr
    openssl x509 -req -in certs/$n.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -days 3 -extfile certs/$n.ext -out certs/$n.crt 2>/dev/null
  done
fi
cat > pebble.json <<J
{"pebble":{"listenAddress":"127.0.0.1:14000","managementListenAddress":"127.0.0.1:15000","certificate":"$E/certs/pebble.crt","privateKey":"$E/certs/pebble.key","httpPort":5002,"tlsPort":5001,"ocspResponderURL":"","externalAccountBindingRequired":false}}
J
bg challtestsrv bin/pebble-challtestsrv -defaultIPv4 127.0.0.1 -defaultIPv6 "" -dns01 127.0.0.1:8053 -management 127.0.0.1:8055 -http01 "" -https01 "" -tlsalpn01 "" -doh ""
PEBBLE_VA_NOSLEEP=1 PEBBLE_WFE_NONCEREJECT=0 bg pebble bin/pebble -config pebble.json -dnsserver 127.0.0.1:8053
sleep 2
bin/mewla-account -grant-key-file grant.pem -print-public-key > grant.pub
bg account bin/mewla-account -addr 127.0.0.1:18090 -db account.db -grant-key-file grant.pem \
  -public-url http://127.0.0.1:18090 -client-domain mewla.test -client-port 19443 \
  -relay "name=local,control=127.0.0.1:18443,server-name=relay.mewla.test,ca-file=$E/certs/ca.crt" \
  -acme-directory https://127.0.0.1:14000/dir -acme-ca-file "$E/certs/ca.crt" \
  -dev-fake-login -dns-provider challtestsrv -challtestsrv-url http://127.0.0.1:8055
bg relay bin/mewla-relay -client-addr 127.0.0.1:19443 -control-addr 127.0.0.1:18443 \
  -operator-addr 127.0.0.1:18081 -tls-cert certs/relay.crt -tls-key certs/relay.key -grant-public-key-file grant.pub
sleep 2
curl -s --cacert certs/ca.crt https://127.0.0.1:15000/roots/0 > certs/pebble-root.pem
for n in challtestsrv pebble account relay; do kill -0 "$(cat pids/$n)" 2>/dev/null && echo "$n up" || { echo "$n DOWN"; tail -5 logs/$n.log; exit 1; }; done
