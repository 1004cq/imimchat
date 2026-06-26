"""
配置管理模块
从环境变量读取所有配置，提供合理的默认值和校验
"""
import os
import secrets


class ConfigError(Exception):
    """配置错误异常"""
    pass


class AppConfig:
    """应用配置类"""

    def __init__(self):
        # 服务配置
        self.SERVICE_PORT = int(os.environ.get('SERVICE_PORT', 9091))
        self.SECRET_KEY = os.environ.get('SECRET_KEY', secrets.token_hex(32))

        # 后端 API 配置
        self.TSDD_API = os.environ.get('TSDD_API', 'http://localhost:8090')
        self.TSDD_SMSCODE = os.environ.get('TSDD_SMSCODE', '123456')
        self.TSDD_API_TIMEOUT = int(os.environ.get('TSDD_API_TIMEOUT', 10))

        # 短信服务配置
        self.SMS_PROVIDER = os.environ.get('SMS_PROVIDER', 'mock')
        self._validate_sms_provider()

        # 阿里云 Dypnsapi 配置
        self.ALIYUN_ACCESS_KEY = os.environ.get('ALIYUN_ACCESS_KEY', '')
        self.ALIYUN_ACCESS_SECRET = os.environ.get('ALIYUN_ACCESS_SECRET', '')
        self.ALIYUN_DYPNS_SIGN_NAME = os.environ.get('ALIYUN_DYPNS_SIGN_NAME', '速通互联验证码')
        self.ALIYUN_DYPNS_TEMPLATE_CODE = os.environ.get('ALIYUN_DYPNS_TEMPLATE_CODE', '100001')
        self.ALIYUN_DYPNS_VALID_TIME = int(os.environ.get('ALIYUN_DYPNS_VALID_TIME', 5))

        if self.SMS_PROVIDER == 'aliyun_dypns':
            self._validate_aliyun_config()

        # 验证码配置
        self.CODE_LENGTH = int(os.environ.get('CODE_LENGTH', 6))
        self.CODE_EXPIRE = int(os.environ.get('CODE_EXPIRE', 300))
        self.CODE_MAX_RETRY = int(os.environ.get('CODE_MAX_RETRY', 3))

        # 频率限制
        self.RATE_LIMIT_REGISTER_PER_HOUR = int(os.environ.get('RATE_LIMIT_REGISTER_PER_HOUR', 5))
        self.RATE_LIMIT_SMS_PER_HOUR = int(os.environ.get('RATE_LIMIT_SMS_PER_HOUR', 3))
        self.RATE_LIMIT_SMS_IP_PER_HOUR = int(os.environ.get('RATE_LIMIT_SMS_IP_PER_HOUR', 10))

        # 存储配置
        self.STORE_BACKEND = os.environ.get('STORE_BACKEND', 'memory')

        # CORS 配置
        self.CORS_ORIGIN = os.environ.get('CORS_ORIGIN', 'https://wed.imim.chat')

        # TRTC 配置
        self.TRTC_SDK_APP_ID = int(os.environ.get('TRTC_SDK_APP_ID', 0))
        self.TRTC_SECRET_KEY = os.environ.get('TRTC_SECRET_KEY', '')
        self.TRTC_ENABLED = bool(self.TRTC_SDK_APP_ID and self.TRTC_SECRET_KEY)
        self.TRTC_USER_SIG_EXPIRE = int(os.environ.get('TRTC_USER_SIG_EXPIRE', 86400 * 7))

    def _validate_sms_provider(self):
        """校验短信服务商配置"""
        valid_providers = ('mock', 'aliyun_dypns')
        if self.SMS_PROVIDER not in valid_providers:
            raise ConfigError(f"无效的 SMS_PROVIDER: {self.SMS_PROVIDER}，有效值: {valid_providers}")

    def _validate_aliyun_config(self):
        """校验阿里云配置完整性"""
        if not self.ALIYUN_ACCESS_KEY:
            raise ConfigError("SMS_PROVIDER=aliyun_dypns 时 ALIYUN_ACCESS_KEY 不能为空")
        if not self.ALIYUN_ACCESS_SECRET:
            raise ConfigError("SMS_PROVIDER=aliyun_dypns 时 ALIYUN_ACCESS_SECRET 不能为空")


# 全局配置单例
_config = None


def get_config() -> AppConfig:
    """获取应用配置单例"""
    global _config
    if _config is None:
        _config = AppConfig()
    return _config
