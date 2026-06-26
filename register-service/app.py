"""
imimchat 安全注册中间件 — 向后兼容入口
新代码请使用 app/ 模块结构
"""
from app import app

if __name__ == '__main__':
    from app.config import get_config
    config = get_config()
    app.run(host='0.0.0.0', port=config.SERVICE_PORT, debug=False)
