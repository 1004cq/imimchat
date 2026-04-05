#!/usr/bin/env python3
"""
imimchat 安全注册中间件服务
功能：
  1. 生成随机验证码并缓存（内存）
  2. 调用短信服务发送验证码（阿里云号码认证 Dypnsapi / mock 模式）
  3. 验证码校验（5分钟有效期、最多3次错误、防重放）
  4. 密码强度校验
  5. 注册频率限制（同IP每小时最多5次）
  6. 调用后端 TangSengDaoDao API 完成注册
"""
import os
import re
import time
import random
import string
import logging
import requests
import json
from flask import Flask, request, jsonify
from collections import defaultdict
from threading import Lock

app = Flask(__name__)
logging.basicConfig(level=logging.INFO, format='%(asctime)s %(levelname)s %(message)s')
logger = logging.getLogger(__name__)

# ===== 配置 =====
TSDD_API = os.environ.get('TSDD_API', 'http://localhost:8090')
# 后端固定验证码（TangSengDaoDao 使用 TS_SMSCODE 配置，默认 123456）
# 中间件负责真实验证码校验，向后端传递此固定码完成注册
TSDD_SMSCODE = os.environ.get('TSDD_SMSCODE', '123456')
# SMS_PROVIDER: mock | aliyun_dypns
SMS_PROVIDER = os.environ.get('SMS_PROVIDER', 'mock')

# 阿里云号码认证服务（Dypnsapi）配置
ALIYUN_ACCESS_KEY = os.environ.get('ALIYUN_ACCESS_KEY', '')
ALIYUN_ACCESS_SECRET = os.environ.get('ALIYUN_ACCESS_SECRET', '')
ALIYUN_DYPNS_SIGN_NAME = os.environ.get('ALIYUN_DYPNS_SIGN_NAME', '速通互联验证码')
ALIYUN_DYPNS_TEMPLATE_CODE = os.environ.get('ALIYUN_DYPNS_TEMPLATE_CODE', '100001')
ALIYUN_DYPNS_VALID_TIME = int(os.environ.get('ALIYUN_DYPNS_VALID_TIME', 5))  # 验证码有效期（分钟）

CODE_EXPIRE = int(os.environ.get('CODE_EXPIRE', 300))       # 验证码有效期（秒），与 VALID_TIME 保持一致
CODE_MAX_RETRY = int(os.environ.get('CODE_MAX_RETRY', 3))   # 最大错误次数
RATE_LIMIT_PER_HOUR = int(os.environ.get('RATE_LIMIT_PER_HOUR', 5))  # 每IP每小时注册上限
RATE_LIMIT_SMS_PER_HOUR = int(os.environ.get('RATE_LIMIT_SMS_PER_HOUR', 3))  # 每手机号每小时短信上限
SERVICE_PORT = int(os.environ.get('SERVICE_PORT', 9091))

# ===== 内存存储（生产环境建议换 Redis）=====
_store_lock = Lock()
_code_store = {}       # phone -> {code, expires_at, retry_count, used}
_rate_store = defaultdict(list)   # ip -> [timestamp, ...]
_sms_rate_store = defaultdict(list)  # phone/ip -> [timestamp, ...]

# ===== 工具函数 =====
def get_client_ip():
    """获取真实客户端 IP（支持 Nginx 代理）"""
    return (request.headers.get('X-Real-IP') or
            request.headers.get('X-Forwarded-For', '').split(',')[0].strip() or
            request.remote_addr)

def generate_code(length=6):
    """生成数字验证码"""
    return ''.join(random.choices(string.digits, k=length))

def is_valid_phone(phone):
    """校验中国大陆手机号"""
    return bool(re.match(r'^1[3-9]\d{9}$', phone))

def check_password_strength(password):
    """
    密码强度校验：
    - 长度 8-32 位
    - 包含大写字母
    - 包含小写字母
    - 包含数字
    """
    if len(password) < 8 or len(password) > 32:
        return False, '密码长度须为 8-32 位'
    if not re.search(r'[A-Z]', password):
        return False, '密码须包含至少一个大写字母'
    if not re.search(r'[a-z]', password):
        return False, '密码须包含至少一个小写字母'
    if not re.search(r'\d', password):
        return False, '密码须包含至少一个数字'
    return True, 'OK'

def check_rate_limit(key, store, limit, window=3600):
    """频率限制检查（滑动窗口）"""
    now = time.time()
    with _store_lock:
        store[key] = [t for t in store[key] if now - t < window]
        if len(store[key]) >= limit:
            return False
        store[key].append(now)
        return True

# ===== 短信发送 =====
def send_sms_mock(phone, code):
    """模拟短信发送（开发/测试用）"""
    logger.info(f'[MOCK SMS] 手机号 {phone} 验证码: {code}')
    return True, '验证码已发送（测试模式，请查看服务器日志）'

def send_sms_aliyun_dypns(phone, code):
    """
    阿里云号码认证服务（Dypnsapi）短信发送
    使用 SendSmsVerifyCode 接口，支持赠送签名和模板，无需企业资质
    """
    try:
        from alibabacloud_dypnsapi20170525.client import Client as DypnsapiClient
        from alibabacloud_tea_openapi import models as open_api_models
        from alibabacloud_dypnsapi20170525 import models as dypnsapi_models

        config = open_api_models.Config(
            access_key_id=ALIYUN_ACCESS_KEY,
            access_key_secret=ALIYUN_ACCESS_SECRET,
            endpoint='dypnsapi.aliyuncs.com'
        )
        client = DypnsapiClient(config)

        send_request = dypnsapi_models.SendSmsVerifyCodeRequest(
            phone_number=phone,
            sign_name=ALIYUN_DYPNS_SIGN_NAME,
            template_code=ALIYUN_DYPNS_TEMPLATE_CODE,
            template_param=json.dumps({'code': code, 'min': str(ALIYUN_DYPNS_VALID_TIME)}),
        )
        resp = client.send_sms_verify_code(send_request)
        result = resp.body.to_map()

        if result.get('Code') == 'OK':
            logger.info(f'[ALIYUN DYPNS] 短信发送成功: {phone[:3]}****{phone[-4:]}')
            return True, '验证码已发送'
        else:
            err_msg = result.get('Message', '短信发送失败')
            err_code = result.get('Code', 'UNKNOWN')
            logger.error(f'[ALIYUN DYPNS] 短信发送失败: {phone[:3]}****{phone[-4:]}，Code={err_code}，Message={err_msg}')
            return False, f'短信发送失败，请稍后重试'

    except Exception as e:
        logger.error(f'[ALIYUN DYPNS] 短信发送异常: {e}')
        return False, '短信服务暂时不可用，请稍后重试'

def send_sms(phone, code):
    """统一短信发送入口"""
    if SMS_PROVIDER == 'aliyun_dypns':
        return send_sms_aliyun_dypns(phone, code)
    return send_sms_mock(phone, code)

# ===== CORS 中间件 =====
@app.after_request
def add_cors_headers(response):
    response.headers['Access-Control-Allow-Origin'] = 'https://wed.imim.chat'
    response.headers['Access-Control-Allow-Methods'] = 'POST, OPTIONS'
    response.headers['Access-Control-Allow-Headers'] = 'Content-Type'
    return response

@app.route('/register/sms', methods=['OPTIONS', 'POST'])
def send_verification_code():
    """
    发送注册验证码
    POST /register/sms
    Body: {"phone": "19988378780"}
    """
    if request.method == 'OPTIONS':
        return '', 204

    data = request.get_json(silent=True) or {}
    phone = str(data.get('phone', '')).strip()

    # 1. 手机号格式校验
    if not is_valid_phone(phone):
        return jsonify({'code': 400, 'msg': '手机号格式不正确'}), 400

    # 2. 检查手机号是否已注册
    try:
        resp = requests.post(
            f'{TSDD_API}/v1/user/register',
            json={'phone': phone, 'zone': '86', 'code': '000000', 'password': 'CheckOnly', 'name': 'check'},
            timeout=5
        )
        resp_data = resp.json()
        if resp_data.get('msg', '').find('已存在') != -1 or resp_data.get('msg', '').find('already') != -1:
            return jsonify({'code': 409, 'msg': '该手机号已注册，请直接登录'}), 409
    except Exception:
        pass

    # 3. 短信频率限制（每手机号每小时最多 3 次）
    if not check_rate_limit(f'sms:{phone}', _sms_rate_store, RATE_LIMIT_SMS_PER_HOUR):
        return jsonify({'code': 429, 'msg': f'验证码发送过于频繁，请 1 小时后再试'}), 429

    # 4. IP 频率限制
    ip = get_client_ip()
    if not check_rate_limit(f'sms_ip:{ip}', _sms_rate_store, 10):
        return jsonify({'code': 429, 'msg': '操作过于频繁，请稍后再试'}), 429

    # 5. 生成验证码并缓存
    code = generate_code(6)
    with _store_lock:
        _code_store[phone] = {
            'code': code,
            'expires_at': time.time() + CODE_EXPIRE,
            'retry_count': 0,
            'used': False
        }

    # 6. 发送短信
    ok, msg = send_sms(phone, code)
    if not ok:
        # 发送失败，清除已缓存的验证码
        with _store_lock:
            _code_store.pop(phone, None)
        return jsonify({'code': 500, 'msg': msg}), 500

    logger.info(f'[SMS] 验证码已发送至 {phone[:3]}****{phone[-4:]}，IP: {ip}，模式: {SMS_PROVIDER}')
    return jsonify({'code': 200, 'msg': f'验证码已发送至 {phone[:3]}****{phone[-4:]}，{CODE_EXPIRE//60} 分钟内有效'})


@app.route('/register/submit', methods=['OPTIONS', 'POST'])
def register():
    """
    提交注册
    POST /register/submit
    Body: {"phone": "19988378780", "code": "123456", "password": "Test123456", "name": "昵称"}
    """
    if request.method == 'OPTIONS':
        return '', 204

    data = request.get_json(silent=True) or {}
    phone = str(data.get('phone', '')).strip()
    code = str(data.get('code', '')).strip()
    password = str(data.get('password', '')).strip()
    name = str(data.get('name', '')).strip()

    # 1. 参数校验
    if not is_valid_phone(phone):
        return jsonify({'code': 400, 'msg': '手机号格式不正确'}), 400
    if not code or len(code) != 6 or not code.isdigit():
        return jsonify({'code': 400, 'msg': '验证码格式不正确'}), 400
    if not name or len(name) < 1 or len(name) > 20:
        return jsonify({'code': 400, 'msg': '昵称长度须为 1-20 个字符'}), 400

    # 2. 密码强度校验
    ok, msg = check_password_strength(password)
    if not ok:
        return jsonify({'code': 400, 'msg': msg}), 400

    # 3. IP 注册频率限制
    ip = get_client_ip()
    if not check_rate_limit(f'reg:{ip}', _rate_store, RATE_LIMIT_PER_HOUR):
        return jsonify({'code': 429, 'msg': f'注册过于频繁，请 1 小时后再试'}), 429

    # 4. 验证码校验
    with _store_lock:
        record = _code_store.get(phone)
        if not record:
            return jsonify({'code': 400, 'msg': '请先获取验证码'}), 400
        if record['used']:
            return jsonify({'code': 400, 'msg': '验证码已使用，请重新获取'}), 400
        if time.time() > record['expires_at']:
            del _code_store[phone]
            return jsonify({'code': 400, 'msg': '验证码已过期，请重新获取'}), 400
        if record['retry_count'] >= CODE_MAX_RETRY:
            del _code_store[phone]
            return jsonify({'code': 400, 'msg': '验证码错误次数过多，请重新获取'}), 400
        if record['code'] != code:
            record['retry_count'] += 1
            remaining = CODE_MAX_RETRY - record['retry_count']
            return jsonify({'code': 400, 'msg': f'验证码错误，还剩 {remaining} 次机会'}), 400
        # 验证码正确，标记为已使用
        record['used'] = True

    # 5. 调用后端 API 注册
    # 注意：后端使用固定验证码（TS_SMSCODE），中间件已完成真实验证码校验
    # 向后端发送 TSDD_SMSCODE 而非用户输入的验证码
    try:
        resp = requests.post(
            f'{TSDD_API}/v1/user/register',
            json={
                'phone': phone,
                'zone': '86',
                'code': TSDD_SMSCODE,  # 使用后端固定验证码
                'password': password,
                'name': name
            },
            timeout=10
        )
        resp_data = resp.json()

        # 注册成功：返回 uid 和 token
        if resp_data.get('uid') or resp_data.get('token'):
            logger.info(f'[REGISTER] 新用户注册成功: {phone[:3]}****{phone[-4:]}，昵称: {name}，IP: {ip}')
            return jsonify({
                'code': 200,
                'msg': '注册成功！',
                'data': {
                    'uid': resp_data.get('uid', ''),
                    'name': resp_data.get('name', name),
                    'token': resp_data.get('token', '')
                }
            })
        else:
            err_msg = resp_data.get('msg', '注册失败，请稍后重试')
            logger.warning(f'[REGISTER] 注册失败: {phone}, 原因: {err_msg}')
            return jsonify({'code': 400, 'msg': err_msg}), 400

    except requests.exceptions.Timeout:
        return jsonify({'code': 500, 'msg': '服务器响应超时，请稍后重试'}), 500
    except Exception as e:
        logger.error(f'[REGISTER] 注册异常: {e}')
        return jsonify({'code': 500, 'msg': '服务器内部错误，请稍后重试'}), 500


@app.route('/register/health', methods=['GET'])
def health():
    return jsonify({'status': 'ok', 'sms_provider': SMS_PROVIDER})


if __name__ == '__main__':
    logger.info(f'imimchat 注册服务启动，端口: {SERVICE_PORT}，短信模式: {SMS_PROVIDER}')
    app.run(host='0.0.0.0', port=SERVICE_PORT, debug=False)
