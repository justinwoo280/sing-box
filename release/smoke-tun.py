#!/usr/bin/env python3
"""Exercise a packaged Linux executable in a disposable network namespace."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    parser.add_argument("--stack", default="lwip", choices=("default", "go", "system", "gvisor", "mixed", "lwip"))
    parser.add_argument("--parent-namespace")
    args = parser.parse_args()
    namespace = os.readlink("/proc/self/ns/net")
    if args.parent_namespace is None:
        subprocess.run(["unshare", "--net", sys.executable, __file__, str(args.binary.resolve()),
                        "--stack", args.stack, "--parent-namespace", namespace], check=True)
        return
    if namespace == args.parent_namespace:
        raise RuntimeError("network namespace isolation failed")
    subprocess.run(["ip", "link", "set", "lo", "up"], check=True)

    tcp = socket.socket()
    tcp.bind(("127.0.0.1", 0))
    tcp.listen()
    port = tcp.getsockname()[1]
    udp = socket.socket(type=socket.SOCK_DGRAM)
    udp.bind(("127.0.0.1", port))

    def serve_tcp():
        while True:
            connection, _ = tcp.accept()
            with connection:
                connection.settimeout(10)
                digest = hashlib.sha256()
                while data := connection.recv(65536):
                    digest.update(data)
                # A reply after EOF detects loss of TCP half-close semantics.
                connection.sendall(digest.digest())

    def serve_udp():
        while True:
            data, source = udp.recvfrom(65535)
            udp.sendto(data, source)

    threading.Thread(target=serve_tcp, daemon=True).start()
    threading.Thread(target=serve_udp, daemon=True).start()
    inbound = {"type": "tun", "interface_name": "sb-smoke", "mtu": 1500,
               "address": ["198.18.0.1/24", "fd00::1/64"], "dns_mode": "disabled"}
    if args.stack != "default":
        inbound["stack"] = args.stack
    config = {
        "log": {"level": "debug"},
        "dns": {"servers": [{"type": "hosts", "tag": "hosts", "predefined": {"stack.test": "203.0.113.7"}}]},
        "inbounds": [inbound],
        "outbounds": [{"type": "direct", "tag": "direct"}],
        "route": {"rules": [
            {"port": 53, "action": "hijack-dns"},
            {"network": ["tcp", "udp"], "action": "route", "outbound": "direct", "override_address": "127.0.0.1"},
        ]},
    }
    with tempfile.TemporaryDirectory(prefix="sing-box-smoke-") as directory:
        path = Path(directory)
        (path / "config.json").write_text(json.dumps(config))
        with (path / "sing-box.log").open("w+") as logfile:
            process = subprocess.Popen([str(args.binary), "run", "-c", str(path / "config.json")],
                                       stdout=logfile, stderr=logfile)
            try:
                deadline = time.monotonic() + 15
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("sing-box exited during startup")
                    links = json.loads(subprocess.check_output(["ip", "-j", "link", "show"]))
                    addresses = subprocess.check_output(["ip", "-6", "addr", "show"]).decode()
                    if any(link["ifname"] == "sb-smoke" and "UP" in link["flags"] for link in links) and "tentative" not in addresses:
                        break
                    if time.monotonic() >= deadline:
                        raise TimeoutError("TUN startup")
                    time.sleep(0.05)
                for family, prefix in (("-4", "198.18.0.0/24"), ("-6", "fd00::/64")):
                    subprocess.run(["ip", family, "route", "replace", prefix, "dev", "sb-smoke"], check=True)
                for host in ("198.18.0.2", "fd00::2"):
                    payload = bytes(range(256)) * 256
                    with socket.create_connection((host, port), timeout=10) as connection:
                        connection.sendall(payload)
                        connection.shutdown(socket.SHUT_WR)
                        with connection.makefile("rb") as reader:
                            assert reader.read() == hashlib.sha256(payload).digest(), "TCP integrity/half-close"
                    family = socket.AF_INET6 if ":" in host else socket.AF_INET
                    with socket.socket(family, socket.SOCK_DGRAM) as connection:
                        connection.settimeout(10)
                        for size in (0, 1200, 8192):
                            data = payload[:size]
                            connection.sendto(data, (host, port))
                            reply, source = connection.recvfrom(65535)
                            assert reply == data and source[:2] == (host, port), "UDP payload/source"
                        query = b"\x12\x34\x01\x00\x00\x01\x00\x00\x00\x00\x00\x00\x05stack\x04test\x00\x00\x01\x00\x01"
                        connection.sendto(query, (host, 53))
                        reply, source = connection.recvfrom(4096)
                        assert reply[:2] == query[:2] and reply[3] & 15 == 0, "DNS response"
                        assert socket.inet_aton("203.0.113.7") in reply and source[:2] == (host, 53), "DNS policy/source"
                    with socket.create_connection((host, 53), timeout=10) as connection:
                        connection.sendall(struct.pack("!H", len(query)) + query)
                        with connection.makefile("rb") as reader:
                            size = struct.unpack("!H", reader.read(2))[0]
                            reply = reader.read(size)
                            assert reply[:2] == query[:2] and socket.inet_aton("203.0.113.7") in reply, "TCP DNS"
                print(f"{args.stack}: IPv4/IPv6 TCP, half-close, UDP fragments and DNS passed")
            except Exception:
                logfile.flush()
                logfile.seek(0)
                print(logfile.read(), file=sys.stderr)
                raise
            finally:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    main()
