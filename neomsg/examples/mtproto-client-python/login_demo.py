#!/usr/bin/env python3
"""NeoMsg Wire WebSocket 客户端示例"""

import json
import struct
import sys

try:
    import websocket
except ImportError:
    print("pip install websocket-client", file=sys.stderr)
    sys.exit(1)

import urllib.request


def login(api_url: str, device_id: str) -> tuple[int, str]:
    body = json.dumps({
        "phone": "13800000000",
        "password": "password",
        "device_id": device_id,
    }).encode()
    req = urllib.request.Request(
        f"{api_url}/v1/auth/login",
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req) as resp:
        data = json.load(resp)
    return data["user_id"], data["access_token"]


def encode_wire_packet(message: dict) -> bytes:
    """简化示例：手动构造 Message protobuf（生产请用 protobuf 库）"""
    # 推荐: from neomsg.v1 import wire_pb2
    raise NotImplementedError(
        "请使用 protoc 生成 Python protobuf，或参考 Go 客户端 wire-client-demo"
    )


def main():
    api = "http://localhost:8090"
    ws_url = "ws://localhost:8080/ws"
    device_id = "demo-python-1"

    user_id, token = login(api, device_id)
    print(f"logged in user_id={user_id}")

    url = f"{ws_url}?token={token}&device_id={device_id}&platform=python"
    ws = websocket.create_connection(url)

    # 发送消息需 protobuf WirePacket，见 wire_ws_client.py 完整版
    print("connected — 请使用 wire_ws_client.py 发送 protobuf 消息")
    ws.close()


if __name__ == "__main__":
    main()
