"""
路由模块
定义所有 API 端点和业务逻辑
"""
import time
import random
import string
import logging
import requests
from flask import Blueprint, request, jsonify, current_app

from .config import get_config
from .validators import (
    validate_phone, validate_code, validate_password, validate_name, sanitize_input
)
from .extensions import get_store
from .sms import send_sms_mock, send_sms_aliyun_dypns, _mask_phone
from .trtc import generate_usersig

logger = logging.getLogger(__name__)

# 蓝图定义
register_bp = Blueprint('register', __name__)
health_bp = Blueprint('health', __name__)
trtc_bp = Blueprint('trtc', __name__)


# ===== 工具函数 =====

def _get_client_ip() -> str:
    """获取真实客户端 IP（支持 Nginx 反向代理）"""
    x_real_ip = request.headers.get('X-Real-IP')
    if x_real_ip:
        return x_real_ip
    x_forwarded_for = request.headers.get('X-Forwarded-For', '')
    if x_forwarded_for:
        return x_forwarded_for.split(',')[0].strip()
    return request.remote_addr or '0.0.0.0'


def _generate_code(length: int = 6) -> str:
    """生成纯数字验证码"""
    return ''.join(random.choices(string.digits, k=length))


def _check_phone_registered(phone: str, api_url: str, timeout: int) -> bool:
    """检查手机号是否已在后端注册，返回 True 表示已注册"""
    try:
        resp = requests.post(
            f'{api_url}/v1/user/register',
            json={
                'phone': phone,
                'zone': '86',
                'code': '000000',
                'password': 'CheckOnlyCheckOnly1',
                'name': 'check'
            },
            timeout=timeout
        )
        resp_data = resp.json()
        msg = resp_data.get('msg', '').lower()
        if '已存在' in msg or 'already' in msg or 'exist' in msg:
            return True
    except Exception:
        pass
    return False


def _json_response(code: int, msg: str, data: dict = None, http_status: int = 200):
    """统一 JSON 响应格式"""
    body = {'code': code, 'msg': msg}
    if data:
        body['data'] = data
    return jsonify(body), http_status


# ===== CORS 处理 =====

@register_bp.before_request
@health_bp.before_request
@trtc_bp.before_request
def handle_options():
    """统一处理 OPTIONS 预检请求"""
    if request.method == 'OPTIONS':
        resp = current_app.make_default_options_response()
        resp.headers['Access-Control-Allow-Origin'] = get_config().CORS_ORIGIN
        resp.headers['Access-Control-Allow-Methods'] = 'POST, OPTIONS, GET'
        resp.headers['Access-Control-Allow-Headers'] = 'Content-Type, Authorization'
        resp.headers['Access-Control-Max-Age'] = '86400'
        return resp


@register_bp.after_request
@health_bp.after_request
@trtc_bp.after_request
def add_cors_headers(response):
    """为所有响应添加 CORS 头"""
    response.headers['Access-Control-Allow-Origin'] = get_config().CORS_ORIGIN
    response.headers['Access-Control-Allow-Methods'] = 'POST, OPTIONS, GET'
    response.headers['Access-Control-Allow-Headers'] = 'Content-Type, Authorization'
    return response


# ===== 健康检查 =====

@health_bp.route('/register/health', methods=['GET'])
def health():
    """健康检查接口"""
    config = get_config()
    return _json_response(200, 'ok', data={
        'status': 'healthy',
        'sms_provider': config.SMS_PROVIDER,
        'store_backend': config.STORE_BACKEND,
    })


# ===== 短信发送 =====

@register_bp.route('/sms', methods=['POST'])
def send_verification_code():
    """
    发送注册验证码
    POST /register/sms
    Body: {"phone": "19988378780"}
    """
    config = get_config()
    store = get_store()

    data = request.get_json(silent=True) or {}
    phone = sanitize_input(str(data.get('phone', '')))

    # 1. 手机号格式校验
    result = validate_phone(phone)
    if not result.valid:
        return _json_response(400, result.message, http_status=400)

    # 2. 检查手机号是否已注册
    if _check_phone_registered(phone, config.TSDD_API, 5):
        return _json_response(409, '该手机号已注册，请直接登录', http_status=409)

    # 3. 短信频率限制（每手机号每小时上限）
    if not store.check_sms_rate(f'sms:{phone}', config.RATE_LIMIT_SMS_PER_HOUR):
        return _json_response(429, '验证码发送过于频繁，请 1 小时后再试', http_status=429)

    # 4. IP 频率限制
    ip = _get_client_ip()
    if not store.check_sms_rate(f'sms_ip:{ip}', config.RATE_LIMIT_SMS_IP_PER_HOUR):
        return _json_response(429, '操作过于频繁，请稍后再试', http_status=429)

    # 5. 生成验证码并缓存
    code = _generate_code(config.CODE_LENGTH)
    store.save_code(phone, code, config.CODE_EXPIRE)

    # 6. 发送短信
    if config.SMS_PROVIDER == 'aliyun_dypns':
        ok, msg = send_sms_aliyun_dypns(
            phone, code,
            config.ALIYUN_ACCESS_KEY,
            config.ALIYUN_ACCESS_SECRET,
            config.ALIYUN_DYPNS_SIGN_NAME,
            config.ALIYUN_DYPNS_TEMPLATE_CODE,
            config.ALIYUN_DYPNS_VALID_TIME,
        )
    else:
        ok, msg = send_sms_mock(phone, code)

    if not ok:
        store.remove_code(phone)
        return _json_response(500, msg, http_status=500)

    logger.info(
        f'[SMS] 验证码已发送至 {_mask_phone(phone)}，IP: {ip}，模式: {config.SMS_PROVIDER}'
    )
    return _json_response(200, f'验证码已发送至 {_mask_phone(phone)}，{config.CODE_EXPIRE // 60} 分钟内有效')


# ===== 注册提交 =====

@register_bp.route('/submit', methods=['POST'])
def register():
    """
    提交注册
    POST /register/submit
    Body: {"phone": "19988378780", "code": "123456", "password": "Test123456", "name": "昵称"}
    """
    config = get_config()
    store = get_store()

    data = request.get_json(silent=True) or {}
    phone = sanitize_input(str(data.get('phone', '')))
    code = sanitize_input(str(data.get('code', '')))
    password = str(data.get('password', ''))
    name = sanitize_input(str(data.get('name', '')))

    # 1. 参数校验
    for validator, value, label in [
        (validate_phone, phone, '手机号'),
        (validate_code, code, '验证码'),
        (validate_name, name, '昵称'),
    ]:
        result = validator(value)
        if not result.valid:
            return _json_response(400, result.message, http_status=400)

    # 2. 密码强度校验
    result = validate_password(password)
    if not result.valid:
        return _json_response(400, result.message, http_status=400)

    # 3. IP 注册频率限制
    ip = _get_client_ip()
    if not store.check_rate(f'reg:{ip}', config.RATE_LIMIT_REGISTER_PER_HOUR):
        return _json_response(429, '注册过于频繁，请 1 小时后再试', http_status=429)

    # 4. 验证码校验
    record = store.get_code_record(phone)
    if not record:
        return _json_response(400, '请先获取验证码', http_status=400)
    if record['used']:
        return _json_response(400, '验证码已使用，请重新获取', http_status=400)
    if time.time() > record['expires_at']:
        store.remove_code(phone)
        return _json_response(400, '验证码已过期，请重新获取', http_status=400)
    if record['retry_count'] >= config.CODE_MAX_RETRY:
        store.remove_code(phone)
        return _json_response(400, '验证码错误次数过多，请重新获取', http_status=400)
    if record['code'] != code:
        retry_count = store.increment_retry(phone)
        remaining = config.CODE_MAX_RETRY - retry_count
        if remaining <= 0:
            store.remove_code(phone)
            return _json_response(400, '验证码错误次数过多，请重新获取', http_status=400)
        return _json_response(400, f'验证码错误，还剩 {remaining} 次机会', http_status=400)

    # 验证码正确，标记为已使用（防重放）
    store.mark_code_used(phone)

    # 5. 调用后端 API 注册
    try:
        resp = requests.post(
            f'{config.TSDD_API}/v1/user/register',
            json={
                'phone': phone,
                'zone': '86',
                'code': config.TSDD_SMSCODE,
                'password': password,
                'name': name
            },
            timeout=config.TSDD_API_TIMEOUT
        )
        resp_data = resp.json()

        if resp_data.get('uid') or resp_data.get('token'):
            logger.info(
                f'[REGISTER] 新用户注册成功: {_mask_phone(phone)}，'
                f'昵称: {name}，IP: {ip}'
            )
            return _json_response(200, '注册成功！', data={
                'uid': resp_data.get('uid', ''),
                'name': resp_data.get('name', name),
                'token': resp_data.get('token', '')
            })
        else:
            err_msg = resp_data.get('msg', '注册失败，请稍后重试')
            logger.warning(f'[REGISTER] 注册失败: {phone}，原因: {err_msg}')
            return _json_response(400, err_msg, http_status=400)

    except requests.exceptions.Timeout:
        logger.error(f'[REGISTER] 后端 API 超时: {phone}')
        return _json_response(500, '服务器响应超时，请稍后重试', http_status=500)
    except requests.exceptions.ConnectionError:
        logger.error(f'[REGISTER] 无法连接后端 API: {config.TSDD_API}')
        return _json_response(500, '服务器内部错误，请稍后重试', http_status=500)
    except Exception as e:
        logger.error(f'[REGISTER] 注册异常: {e}')
        return _json_response(500, '服务器内部错误，请稍后重试', http_status=500)


# ===== TRTC UserSig 生成 =====

@trtc_bp.route('/trtc/usersig', methods=['POST'])
def get_usersig():
    """
    生成 TRTC UserSig
    POST /trtc/usersig
    Headers: Authorization: Bearer <token>
    Body: {"userId": "user_xxx"}
    """
    config = get_config()

    if not config.TRTC_ENABLED:
        return _json_response(503, 'TRTC 服务未配置', http_status=503)

    data = request.get_json(silent=True) or {}
    user_id = str(data.get('userId', '')).strip()

    if not user_id:
        return _json_response(400, 'userId 不能为空', http_status=400)

    # 简单鉴权：检查 Authorization header
    auth_header = request.headers.get('Authorization', '')
    if not auth_header:
        return _json_response(401, '未授权，请先登录', http_status=401)

    try:
        user_sig = generate_usersig(
            sdk_app_id=config.TRTC_SDK_APP_ID,
            secret_key=config.TRTC_SECRET_KEY,
            user_id=user_id,
            expire=config.TRTC_USER_SIG_EXPIRE,
        )

        logger.info(f'[TRTC] UserSig 已生成: userId={user_id}')

        return _json_response(200, 'ok', data={
            'sdkAppId': config.TRTC_SDK_APP_ID,
            'userId': user_id,
            'userSig': user_sig,
            'expire': config.TRTC_USER_SIG_EXPIRE,
        })

    except Exception as e:
        logger.error(f'[TRTC] UserSig 生成异常: {e}')
        return _json_response(500, 'UserSig 生成失败', http_status=500)

