import os
import sys
import time
import unittest
from unittest.mock import patch

ROOT_DIR = os.path.dirname(os.path.dirname(__file__))
if ROOT_DIR not in sys.path:
    sys.path.insert(0, ROOT_DIR)

import app as register_app


class RegisterServiceTestCase(unittest.TestCase):
    def setUp(self):
        register_app.app.config['TESTING'] = True
        self.client = register_app.app.test_client()
        self.reset_stores()

    def tearDown(self):
        self.reset_stores()

    def reset_stores(self):
        with register_app._store_lock:
            register_app._code_store.clear()
            register_app._rate_store.clear()
            register_app._sms_rate_store.clear()

    def test_generate_code_uses_six_digits(self):
        code = register_app.generate_code()

        self.assertEqual(6, len(code))
        self.assertTrue(code.isdigit())

    def test_send_sms_stores_verification_code(self):
        with patch.object(register_app, 'is_phone_already_registered', return_value=False), \
                patch.object(register_app, 'send_sms', return_value=(True, '验证码已发送')):
            resp = self.client.post('/register/sms', json={'phone': '13900000001'})

        self.assertEqual(200, resp.status_code)
        body = resp.get_json()
        self.assertEqual(200, body['code'])
        with register_app._store_lock:
            record = register_app._code_store['13900000001']
        self.assertEqual(6, len(record['code']))
        self.assertFalse(record['used'])

    def test_register_submits_fixed_backend_code_after_verification(self):
        phone = '13900000002'
        with register_app._store_lock:
            register_app._code_store[phone] = {
                'code': '123456',
                'expires_at': time.time() + 60,
                'retry_count': 0,
                'used': False,
            }

        with patch.object(register_app, 'call_tsdd_register', return_value=(None, {
            'uid': 'u_1',
            'name': '测试用户',
            'token': 'token_1',
        })) as mock_register:
            resp = self.client.post('/register/submit', json={
                'phone': phone,
                'code': '123456',
                'password': 'Test123456',
                'name': '测试用户',
            })

        self.assertEqual(200, resp.status_code)
        body = resp.get_json()
        self.assertEqual('u_1', body['data']['uid'])
        payload = mock_register.call_args.args[0]
        self.assertEqual(register_app.TSDD_SMSCODE, payload['code'])
        with register_app._store_lock:
            self.assertTrue(register_app._code_store[phone]['used'])

    def test_wrong_code_is_limited_and_removed(self):
        phone = '13900000003'
        with register_app._store_lock:
            register_app._code_store[phone] = {
                'code': '123456',
                'expires_at': time.time() + 60,
                'retry_count': register_app.CODE_MAX_RETRY - 1,
                'used': False,
            }

        resp = self.client.post('/register/submit', json={
            'phone': phone,
            'code': '654321',
            'password': 'Test123456',
            'name': '测试用户',
        })

        self.assertEqual(400, resp.status_code)
        self.assertIn('还剩 0 次机会', resp.get_json()['msg'])

        resp = self.client.post('/register/submit', json={
            'phone': phone,
            'code': '654321',
            'password': 'Test123456',
            'name': '测试用户',
        })

        self.assertEqual(400, resp.status_code)
        self.assertIn('错误次数过多', resp.get_json()['msg'])
        with register_app._store_lock:
            self.assertNotIn(phone, register_app._code_store)

    def test_health_cleans_expired_records(self):
        with register_app._store_lock:
            register_app._code_store['13900000004'] = {
                'code': '123456',
                'expires_at': time.time() - 1,
                'retry_count': 0,
                'used': False,
            }
            register_app._sms_rate_store['sms:13900000004'] = [time.time() - register_app.RATE_LIMIT_WINDOW - 1]

        resp = self.client.get('/register/health')

        self.assertEqual(200, resp.status_code)
        stores = resp.get_json()['stores']
        self.assertEqual(0, stores['codes'])
        self.assertEqual(0, stores['sms_rate_keys'])


if __name__ == '__main__':
    unittest.main()
