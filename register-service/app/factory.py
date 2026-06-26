"""
应用工厂模块
负责创建和配置 Flask 应用实例
"""
import logging
from flask import Flask
from .config import get_config
from .extensions import init_extensions


def create_app() -> Flask:
    app = Flask(__name__)

    # 加载配置
    config = get_config()
    app.config.from_object(config)

    # 初始化日志
    _setup_logging(app)

    # 初始化扩展
    init_extensions(app)

    # 注册蓝图
    _register_blueprints(app)

    # 注册错误处理
    _register_error_handlers(app)

    # 注册请求钩子
    _register_hooks(app)

    app.logger.info(
        f"imimchat 注册服务启动，端口: {config.SERVICE_PORT}，"
        f"短信模式: {config.SMS_PROVIDER}"
    )
    return app


def _setup_logging(app):
    """配置结构化日志"""
    handler = logging.StreamHandler()
    handler.setFormatter(logging.Formatter(
        '%(asctime)s %(levelname)s [%(name)s] %(message)s',
        datefmt='%Y-%m-%d %H:%M:%S'
    ))
    app.logger.addHandler(handler)
    app.logger.setLevel(logging.INFO)
    # 降低第三方库日志噪音
    logging.getLogger('werkzeug').setLevel(logging.WARNING)


def _register_blueprints(app):
    """注册所有 API 蓝图"""
    from .routes import register_bp, health_bp, trtc_bp
    app.register_blueprint(register_bp, url_prefix='/register')
    app.register_blueprint(health_bp)
    app.register_blueprint(trtc_bp)


def _register_error_handlers(app):
    """注册全局错误处理器"""
    from flask import jsonify

    @app.errorhandler(400)
    def bad_request(e):
        return jsonify({'code': 400, 'msg': str(e.description) if hasattr(e, 'description') else '请求参数错误'}), 400

    @app.errorhandler(404)
    def not_found(e):
        return jsonify({'code': 404, 'msg': '接口不存在'}), 404

    @app.errorhandler(405)
    def method_not_allowed(e):
        return jsonify({'code': 405, 'msg': '请求方法不允许'}), 405

    @app.errorhandler(429)
    def too_many_requests(e):
        return jsonify({'code': 429, 'msg': '请求过于频繁，请稍后再试'}), 429

    @app.errorhandler(500)
    def internal_error(e):
        app.logger.error(f"未捕获的服务器错误: {e}")
        return jsonify({'code': 500, 'msg': '服务器内部错误，请稍后重试'}), 500


def _register_hooks(app):
    """注册请求前后钩子"""
    from flask import request

    @app.before_request
    def log_request():
        app.logger.debug(f"[REQ] {request.method} {request.path} from {request.remote_addr}")

    @app.after_request
    def add_security_headers(response):
        response.headers['X-Content-Type-Options'] = 'nosniff'
        response.headers['X-Frame-Options'] = 'DENY'
        response.headers['X-XSS-Protection'] = '1; mode=block'
        return response
