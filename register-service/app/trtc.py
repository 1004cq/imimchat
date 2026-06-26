"""
TRTC UserSig 生成模块
基于 HMAC-SHA256 签名算法生成 TRTC 鉴权票据
参考文档: https://cloud.tencent.com/document/product/647/17275
"""
import hmac
import hashlib
import base64
import zlib
import json
import time
import logging

logger = logging.getLogger(__name__)


def _base64_encode(data: bytes) -> str:
    """URL-safe Base64 编码"""
    return base64.b64encode(data).decode('utf-8').rstrip('=')


def generate_usersig(sdk_app_id: int, secret_key: str, user_id: str, expire: int = 86400 * 7) -> str:
    """
    生成 TRTC UserSig (非官方精简实现)

    参数:
        sdk_app_id: TRTC 应用的 SDKAppID
        secret_key: TRTC 应用的 SecretKey
        user_id: 用户标识（通常为手机号或 UID）
        expire: 签名有效期（秒），默认 7 天

    返回:
        Base64 编码的 UserSig 字符串
    """
    current = int(time.time())
    sig_doc = {
        'TLS.ver': '2.0',
        'TLS.identifier': str(user_id),
        'TLS.sdkappid': int(sdk_app_id),
        'TLS.expire': int(expire),
        'TLS.time': current,
    }

    # 序列化为 JSON
    raw = json.dumps(sig_doc, separators=(',', ':'))

    # 使用官方 TLSSigAPIv2 的生成逻辑
    prefix = b'\x00\x00\x00\x00'  # 固定前缀
    raw_bytes = prefix + raw.encode('utf-8')

    # zlib 压缩
    compressed = zlib.compress(raw_bytes, level=zlib.Z_BEST_SPEED)

    # HMAC-SHA256 签名
    signature = hmac.new(
        secret_key.encode('utf-8'),
        compressed,
        hashlib.sha256
    ).digest()

    # 拼接: 压缩数据 + 签名
    result = compressed + signature

    return _base64_encode(result)


def verify_usersig(sdk_app_id: int, secret_key: str, user_sig: str) -> dict | None:
    """
    验证 UserSig 是否有效（用于调试）

    返回:
        dict 包含解析后的签名信息，验证失败返回 None
    """
    try:
        # Base64 解码（补齐 padding）
        padding = 4 - len(user_sig) % 4
        if padding != 4:
            user_sig += '=' * padding

        data = base64.b64decode(user_sig)

        # 分离压缩数据和签名（最后 32 字节为 HMAC-SHA256 签名）
        compressed = data[:-32]
        expected_sig = data[-32:]

        # 验证签名
        actual_sig = hmac.new(
            secret_key.encode('utf-8'),
            compressed,
            hashlib.sha256
        ).digest()

        if not hmac.compare_digest(actual_sig, expected_sig):
            logger.warning('[TRTC] UserSig 签名验证失败')
            return None

        # 解压并解析 JSON
        decompressed = zlib.decompress(compressed)
        raw = decompressed[4:]  # 去掉 4 字节前缀
        sig_doc = json.loads(raw.decode('utf-8'))

        # 检查是否过期
        expire_time = sig_doc.get('TLS.time', 0) + sig_doc.get('TLS.expire', 0)
        if int(time.time()) > expire_time:
            logger.warning('[TRTC] UserSig 已过期')
            return None

        return sig_doc

    except Exception as e:
        logger.error(f'[TRTC] UserSig 验证异常: {e}')
        return None
