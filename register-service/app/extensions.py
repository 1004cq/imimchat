"""
扩展模块
统一管理第三方库的初始化和依赖注入
"""
from .store import MemoryStore


_store_instance = None


def init_extensions(app):
    """初始化所有扩展"""
    global _store_instance
    _store_instance = MemoryStore()
    app.logger.info(f"存储后端已初始化: 内存模式")
    return _store_instance


def get_store():
    """获取存储实例"""
    global _store_instance
    if _store_instance is None:
        _store_instance = MemoryStore()
    return _store_instance
