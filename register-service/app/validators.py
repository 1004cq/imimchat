"""
校验器模块
提供手机号、密码、验证码等输入校验
"""
import re
import string
from dataclasses import dataclass


@dataclass
class ValidationResult:
    """校验结果"""
    valid: bool
    message: str = ""


def validate_phone(phone: str) -> ValidationResult:
    """校验中国大陆手机号"""
    if not phone:
        return ValidationResult(False, "手机号不能为空")
    if not re.match(r'^1[3-9]\d{9}$', phone):
        return ValidationResult(False, "手机号格式不正确")
    return ValidationResult(True, "OK")


def validate_code(code: str) -> ValidationResult:
    """校验验证码格式"""
    if not code:
        return ValidationResult(False, "验证码不能为空")
    if len(code) != 6 or not code.isdigit():
        return ValidationResult(False, "验证码格式不正确（须为6位数字）")
    return ValidationResult(True, "OK")


def validate_password(password: str) -> ValidationResult:
    """
    密码强度校验：
    - 长度 8-32 位
    - 包含大写字母、小写字母、数字
    """
    if not password:
        return ValidationResult(False, "密码不能为空")
    if len(password) < 8:
        return ValidationResult(False, "密码长度须至少 8 位")
    if len(password) > 32:
        return ValidationResult(False, "密码长度须不超过 32 位")
    if not re.search(r'[A-Z]', password):
        return ValidationResult(False, "密码须包含至少一个大写字母")
    if not re.search(r'[a-z]', password):
        return ValidationResult(False, "密码须包含至少一个小写字母")
    if not re.search(r'\d', password):
        return ValidationResult(False, "密码须包含至少一个数字")
    # 额外安全检查：禁止常见弱密码
    common_passwords = {'Password123', 'Test123456', 'Admin123456', 'Qwerty123'}
    if password in common_passwords:
        return ValidationResult(False, "密码过于常见，请使用更强的密码")
    return ValidationResult(True, "OK")


def validate_name(name: str) -> ValidationResult:
    """校验昵称"""
    if not name:
        return ValidationResult(False, "昵称不能为空")
    name = name.strip()
    if len(name) < 1:
        return ValidationResult(False, "昵称不能为空")
    if len(name) > 20:
        return ValidationResult(False, "昵称长度须不超过 20 个字符")
    # 过滤不可见字符
    if any(c in string.whitespace for c in name if c != ' '):
        return ValidationResult(False, "昵称包含无效字符")
    return ValidationResult(True, "OK")


def sanitize_input(text: str) -> str:
    """输入清理：去除首尾空白和不可见字符"""
    return text.strip()
