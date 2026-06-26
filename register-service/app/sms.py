"""
短信服务模块
支持 mock 模式和阿里云号码认证服务（Dypnsapi）
"""
import json
import logging

logger = logging.getLogger(__name__)


def send_sms_mock(phone: str, code: str) -> tuple[bool, str]:
    """模拟短信发送（开发/测试用）"""
    logger.info(f'[MOCK SMS] 手机号 {_mask_phone(phone)} 验证码: {code}')
    return True, '验证码已发送（测试模式，请查看服务器日志）'


def send_sms_aliyun_dypns(
    phone: str,
    code: str,
    access_key: str,
    access_secret: str,
    sign_name: str,
    template_code: str,
    valid_time: int,
) -> tuple[bool, str]:
    """
    阿里云号码认证服务（Dypnsapi）短信发送
    """
    try:
        from alibabacloud_dypnsapi20170525.client import Client as DypnsapiClient
        from alibabacloud_tea_openapi import models as open_api_models
        from alibabacloud_dypnsapi20170525 import models as dypnsapi_models

        config = open_api_models.Config(
            access_key_id=access_key,
            access_key_secret=access_secret,
            endpoint='dypnsapi.aliyuncs.com'
        )
        client = DypnsapiClient(config)

        send_request = dypnsapi_models.SendSmsVerifyCodeRequest(
            phone_number=phone,
            sign_name=sign_name,
            template_code=template_code,
            template_param=json.dumps({'code': code, 'min': str(valid_time)}),
        )
        resp = client.send_sms_verify_code(send_request)
        result = resp.body.to_map()

        if result.get('Code') == 'OK':
            logger.info(f'[ALIYUN DYPNS] 短信发送成功: {_mask_phone(phone)}')
            return True, '验证码已发送'
        else:
            err_msg = result.get('Message', '短信发送失败')
            err_code = result.get('Code', 'UNKNOWN')
            logger.error(
                f'[ALIYUN DYPNS] 短信发送失败: {_mask_phone(phone)}，'
                f'Code={err_code}，Message={err_msg}'
            )
            return False, '短信发送失败，请稍后重试'

    except Exception as e:
        logger.error(f'[ALIYUN DYPNS] 短信发送异常: {e}')
        return False, '短信服务暂时不可用，请稍后重试'


def _mask_phone(phone: str) -> str:
    """手机号脱敏显示"""
    if len(phone) >= 7:
        return f'{phone[:3]}****{phone[-4:]}'
    return phone
