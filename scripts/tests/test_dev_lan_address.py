"""The Dev app is built with the address a phone on the LAN reaches this host at (client/scripts/lan-address.sh).

Not the route to the internet: on a host whose traffic goes through a proxy's TUN (mihomo, 198.18.0.1) that
was the TUN, and the Dev APK went out with no server the phone could reach. The picker skips tunnels and
virtual interfaces, prefers the interface of a default route, and fails loudly when nothing is left; then
dev-android.sh does not build. DEV_HOST still wins.
"""

from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPTS = ROOT / "client/scripts"

# `ip -4 -o addr show up scope global` and `ip -4 route show default` on the host where it went wrong.
THIS_HOST = """\
2: enp11s0    inet 192.168.110.223/24 brd 192.168.110.255 scope global dynamic noprefixroute enp11s0\\       valid_lft 6827sec preferred_lft 6827sec
4: tailscale0    inet 100.106.5.18/32 scope global tailscale0\\       valid_lft forever preferred_lft forever
7: br-6c6117e104a1    inet 172.24.0.1/16 brd 172.24.255.255 scope global br-6c6117e104a1\\       valid_lft forever preferred_lft forever
9: docker0    inet 172.17.0.1/16 brd 172.17.255.255 scope global docker0\\       valid_lft forever preferred_lft forever
21: Meta    inet 198.18.0.1/30 brd 198.18.0.3 scope global Meta\\       valid_lft forever preferred_lft forever
"""
THIS_HOST_DEFAULT = "default via 192.168.110.1 dev enp11s0 proto dhcp src 192.168.110.223 metric 100\n"

FAKE_IP = """#!/bin/sh
case "$*" in
  *route*) cat "$FAKE_NET/route" ;;
  *) cat "$FAKE_NET/addr" ;;
esac
"""


class Host:
    """A scratch host: a fake `ip`, a virtual-interface directory, and copies of the scripts with a fake gradlew."""

    def __init__(self, addr, route="", virtual=()):
        self.dir = Path(tempfile.mkdtemp())
        net = self.dir / "net"
        (net / "virtual").mkdir(parents=True)
        (net / "addr").write_text(addr)
        (net / "route").write_text(route)
        for name in virtual:
            (net / "virtual" / name).mkdir()
        bin_dir = self.dir / "bin"
        bin_dir.mkdir()
        (bin_dir / "ip").write_text(FAKE_IP)
        (bin_dir / "ip").chmod(0o755)
        scripts = self.dir / "client/scripts"
        scripts.mkdir(parents=True)
        for name in ("lan-address.sh", "dev-android.sh"):
            shutil.copy2(SCRIPTS / name, scripts / name)
        android = self.dir / "client/android"
        android.mkdir()
        # Records how it was called and stops dev-android.sh there (no build, no nsh dev).
        (android / "gradlew").write_text('#!/bin/sh\necho "$@" > "$FAKE_NET/gradlew"\nexit 7\n')
        (android / "gradlew").chmod(0o755)
        self.env = {
            "PATH": f"{bin_dir}:/usr/bin:/bin",
            "FAKE_NET": str(net),
            "MEWLA_VIRTUAL_NET_DIR": str(net / "virtual"),
        }

    def run(self, script, **env):
        return subprocess.run([str(self.dir / "client/scripts" / script)], env={**self.env, **env},
                              capture_output=True, text=True, timeout=30)

    @property
    def gradlew_args(self):
        path = Path(self.env["FAKE_NET"]) / "gradlew"
        return path.read_text().strip() if path.exists() else None

    def close(self):
        shutil.rmtree(self.dir)


class LanAddress(unittest.TestCase):
    def host(self, *args, **kwargs):
        host = Host(*args, **kwargs)
        self.addCleanup(host.close)
        return host

    def test_this_host_gets_its_lan_address_not_the_tun_tailscale_or_docker(self):
        run = self.host(THIS_HOST, THIS_HOST_DEFAULT, virtual=("Meta", "docker0", "br-6c6117e104a1")).run("lan-address.sh")
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(run.stdout, "192.168.110.223\n")
        self.assertIn("LAN address: 192.168.110.223 (enp11s0)", run.stderr)

    def test_a_tunnel_on_a_private_range_is_skipped_by_being_virtual_whatever_its_name(self):
        # sing-box and clash give their TUN 172.19.0.1; only /sys says it is virtual.
        addr = "3: singbox    inet 172.19.0.1/30 scope global singbox\n" + THIS_HOST
        run = self.host(addr, "default dev singbox\n" + THIS_HOST_DEFAULT, virtual=("singbox",)).run("lan-address.sh")
        self.assertEqual(run.stdout, "192.168.110.223\n", run.stderr)

    def test_the_interface_of_a_default_route_wins_and_the_others_are_named(self):
        addr = "2: eth0    inet 10.0.0.5/24 scope global eth0\n3: wlan0    inet 192.168.1.20/24 scope global wlan0\n"
        run = self.host(addr, "default via 192.168.1.1 dev wlan0\n").run("lan-address.sh")
        self.assertEqual(run.stdout, "192.168.1.20\n")
        self.assertIn("also 10.0.0.5 (eth0)", run.stderr)
        self.assertIn("DEV_HOST", run.stderr)

    def test_with_no_default_route_on_the_lan_the_first_lan_address_is_taken(self):
        addr = "2: eth0    inet 10.0.0.5/24 scope global eth0\n3: wlan0    inet 172.16.4.9/24 scope global wlan0\n"
        run = self.host(addr, "default dev Meta\n").run("lan-address.sh")
        self.assertEqual(run.stdout, "10.0.0.5\n")

    def test_only_tunnels_and_public_addresses_fail_loudly(self):
        addr = (
            "2: eth0    inet 203.0.113.5/24 scope global eth0\n"
            "4: tailscale0    inet 100.106.5.18/32 scope global tailscale0\n"
            "5: wg0    inet 10.8.0.2/24 scope global wg0\n"
            "6: virbr0    inet 192.168.122.1/24 scope global virbr0\n"
            "21: Meta    inet 198.18.0.1/30 scope global Meta\n"
        )
        run = self.host(addr, "default dev Meta\n", virtual=("Meta",)).run("lan-address.sh")
        self.assertEqual(run.returncode, 1)
        self.assertEqual(run.stdout, "")
        self.assertIn("No private LAN address", run.stderr)
        self.assertIn("skipped wg0 10.8.0.2", run.stderr)
        self.assertIn("Set DEV_HOST", run.stderr)

    def test_172_outside_16_to_31_is_not_private(self):
        run = self.host("2: eth0    inet 172.32.0.4/24 scope global eth0\n").run("lan-address.sh")
        self.assertEqual(run.returncode, 1)


class DevAndroidBuildsWithTheLanAddress(unittest.TestCase):
    def host(self, *args, **kwargs):
        host = Host(*args, **kwargs)
        self.addCleanup(host.close)
        return host

    def test_it_builds_with_the_lan_address_and_says_which(self):
        host = self.host(THIS_HOST, THIS_HOST_DEFAULT, virtual=("Meta", "docker0", "br-6c6117e104a1"))
        run = host.run("dev-android.sh")
        self.assertIn("-Pmewla.devServer=ws://192.168.110.223:8790/ws", host.gradlew_args or "", run.stderr)
        self.assertIn("LAN address: 192.168.110.223 (enp11s0)", run.stderr)

    def test_with_no_lan_address_it_does_not_build(self):
        host = self.host("21: Meta    inet 198.18.0.1/30 scope global Meta\n", "default dev Meta\n", virtual=("Meta",))
        run = host.run("dev-android.sh")
        self.assertEqual(run.returncode, 1)
        self.assertIsNone(host.gradlew_args)
        self.assertIn("Not building", run.stderr)

    def test_dev_host_wins(self):
        host = self.host("21: Meta    inet 198.18.0.1/30 scope global Meta\n", virtual=("Meta",))
        run = host.run("dev-android.sh", DEV_HOST="10.1.2.3", PORT="8800")
        self.assertIn("-Pmewla.devServer=ws://10.1.2.3:8800/ws", host.gradlew_args or "", run.stderr)
        self.assertIn("LAN address: 10.1.2.3 (DEV_HOST)", run.stderr)


if __name__ == "__main__":
    unittest.main()
