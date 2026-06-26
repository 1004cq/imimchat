#!/usr/bin/env python3
"""
NeoMsg Wire WebSocket 客户端（需先生成 Python protobuf）

  cd neomsg && ./scripts/gen-proto.sh
  pip install websocket-client protobuf requests

  python examples/mtproto-client-python/wire_ws_client.py
"""

import json
import struct
import sys
import time

import requests

try:
    import websocket
except ImportError:
    print("pip install websocket-client requests", file=sys.stderr)
    sys.exit(1)

# 若已生成 Python pb，取消注释：
# from neomsg.v1 import wire_pb2


def frame_wire_packet(pkt_bytes: bytes) -> bytes:
    return struct.pack(">I", len(pkt_bytes)) + pkt_bytes


def login(api_url: str, device_id: str) -> tuple[int, str]:
    r = requests.post(
        f"{api_url}/v1/auth/login",
        json={"phone": "13800000000", "password": "password", "device_id": device_id},
        timeout=10,
    )
    r.raise_for_status()
    data = r.json()
    return data["user_id"], data["access_token"]


def build_message_packet(chat_id: int, from_id: int, content: str) -> bytes:
    """手工最小 protobuf 编码 Message（仅演示；生产用 wire_pb2）"""
    def enc_varint(v: int) -> bytes:
        out = bytearray()
        while v > 0x7F:
            out.append((v & 0x7F) | 0x80)
            v >>= 7
        out.append(v & 0x7F)
        return bytes(out)

    def enc_field(num: int, wire: int, data: bytes) -> bytes:
        return enc_varint((num << 3) | wire) + data

    msg = b""
    msg += enc_field(2, 0, enc_varint(chat_id))
    msg += enc_field(3, 0, enc_varint(from_id))
    msg += enc_field(5, 2, enc_varint(len(content)) + content.encode())
    msg += enc_field(6, 0, enc_varint(0))
    msg += enc_field(9, 0, enc_varint(int(time.time() * 1000)))

    # WirePacket.message = field 1 (length-delimited)
    wire = enc_field(1, 2, enc_varint(len(msg)) + msg)
    return wire


def parse_ack(frame: bytes) -> dict:
    length = struct.unpack(">I", frame[:4])[0]
    payload = frame[4 : 4 + length]
    # 简化：查找 success bool（field 3）
    return {"raw_len": length, "success": b"\x08\x01" in payload}


def main():
    api = "http://localhost:8090"
    ws_url = "ws://localhost:8080/ws"
    device_id = "demo-python-ws"
    chat_id = 1

    user_id, token = login(api, device_id)
    print(f"login ok user_id={user_id}")

    url = f"{ws_url}?token={token}&device_id={device_id}&platform=python"
    ws = websocket.create_connection(url, timeout=10)

    pkt = build_message_packet(chat_id, user_id, "hello from python wire client")
    ws.send_binary(frame_wire_packet(pkt))

    resp = ws.recv()
    if isinstance(resp, str):
        resp = resp.encode()
    print("ack:", parse_ack(resp))
    ws.close()


if __name__ == "__main__":
    main()
