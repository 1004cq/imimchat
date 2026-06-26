"""
存储模块
提供验证码和频率限制的内存存储，支持后续切换到 Redis
"""
import time
from threading import Lock
from collections import defaultdict


class MemoryStore:
    """内存存储（单进程安全，支持后续替换为 Redis）"""

    def __init__(self):
        self._lock = Lock()
        self._code_store: dict = {}          # phone -> {code, expires_at, retry_count, used}
        self._rate_store: dict = defaultdict(list)     # key -> [timestamp, ...]
        self._sms_rate_store: dict = defaultdict(list)  # key -> [timestamp, ...]

    # ---- 验证码操作 ----

    def save_code(self, phone: str, code: str, expire_seconds: int):
        """保存验证码"""
        with self._lock:
            self._code_store[phone] = {
                'code': code,
                'expires_at': time.time() + expire_seconds,
                'retry_count': 0,
                'used': False,
            }

    def get_code_record(self, phone: str) -> dict | None:
        """获取验证码记录"""
        return self._code_store.get(phone)

    def mark_code_used(self, phone: str):
        """标记验证码已使用"""
        with self._lock:
            if phone in self._code_store:
                self._code_store[phone]['used'] = True

    def increment_retry(self, phone: str) -> int:
        """增加错误次数，返回剩余次数"""
        with self._lock:
            if phone in self._code_store:
                self._code_store[phone]['retry_count'] += 1
                return self._code_store[phone]['retry_count']
        return 0

    def remove_code(self, phone: str):
        """删除验证码记录"""
        with self._lock:
            self._code_store.pop(phone, None)

    # ---- 频率限制 ----

    def check_rate(self, key: str, limit: int, window_seconds: int = 3600) -> bool:
        """检查频率限制，返回 True 表示允许"""
        now = time.time()
        with self._lock:
            store = self._rate_store
            store[key] = [t for t in store[key] if now - t < window_seconds]
            if len(store[key]) >= limit:
                return False
            store[key].append(now)
            return True

    def check_sms_rate(self, key: str, limit: int, window_seconds: int = 3600) -> bool:
        """检查短信频率限制，返回 True 表示允许"""
        now = time.time()
        with self._lock:
            store = self._sms_rate_store
            store[key] = [t for t in store[key] if now - t < window_seconds]
            if len(store[key]) >= limit:
                return False
            store[key].append(now)
            return True

    # ---- 清理 ----

    def cleanup_expired(self):
        """清理过期验证码（可定时调用）"""
        now = time.time()
        with self._lock:
            expired = [p for p, r in self._code_store.items() if r['expires_at'] < now]
            for phone in expired:
                del self._code_store[phone]
        return len(expired)
