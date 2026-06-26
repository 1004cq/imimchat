#!/usr/bin/env python3
"""
NeoMsg MTProto 客户端骨架（TCP Abridged + JWT bind）

完整 DH 握手与 AES-IGE 建议使用 Go 参考实现：
  neomsg/backend/cmd/mtproto-client-demo

本脚本演示：登录拿 JWT → 获取 MTProto 配置 → TCP 连接准备
"""

import json
import socket
import sys

import requests

ABRIDGED = b"\xef"


def login(api_url: str, device_id: str) -> tuple[int, str]:
    r = requests.post(
        f"{api_url}/v1/auth/login",
        json={"phone": "13800000000", "password": "password", "device_id": device_id},
        timeout=10,
    )
    r.raise_for_status()
    d = r.json()
    return d["user_id"], d["access_token"]


def fetch_mtproto_config(config_url: str) -> dict:
    r = requests.get(config_url, timeout=10)
    r.raise_for_status()
    return r.json()


def connect_abridged(host: str, port: int) -> socket.socket:
    s = socket.create_connection((host, port), timeout=10)
    s.sendall(ABRIDGED)
    return s


def main():
    api = "http://localhost:8090"
    config_url = "http://localhost:10444/config"
    host, port = "localhost", 10443
    device_id = "demo-python-mtproto"

    user_id, token = login(api, device_id)
    cfg = fetch_mtproto_config(config_url)
    print(f"user_id={user_id} rsa_fingerprint={cfg.get('rsa_fingerprint')}")

    sock = connect_abridged(host, port)
    print("TCP connected (abridged). Next steps:")
    print("  1. DH handshake (see Go mtprotoclient)")
    print("  2. encrypted bindSession(user_id, device_id, jwt)")
    print("  3. encrypted invokeWire(WirePacket{Message})")
    sock.close()


if __name__ == "__main__":
    main()
