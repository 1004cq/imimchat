/**
 * imimchat 移动端深度适配脚本 v3
 * 功能：
 *   1. 修复移动端布局（隐藏侧边栏、全屏内容区）
 *   2. 微信风格底部导航栏（消息、联系人、发现、我）
 *   3. 聊天页面滑入/滑出动画
 *   4. 修复登录 username 格式（0086xxx -> 86xxx）
 *   5. 深色/浅色模式切换
 */
(function() {
  'use strict';

  // ===== 立即执行：修复 username 格式 =====
  // 前端会将手机号转换为 008619xxxxxxxxx 格式（带国际拨号前缀 00）
  // 正确格式应为 8619xxxxxxxxx（86 是中国国家区号）
  // 将 0086 开头的 username 修改为 86 开头
  (function fixLoginUsername() {
    var origOpen = XMLHttpRequest.prototype.open;
    var origSend = XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.open = function(method, url) {
      this._xhrUrl = url;
      return origOpen.apply(this, arguments);
    };
    XMLHttpRequest.prototype.send = function(body) {
      if (this._xhrUrl && this._xhrUrl.toString().indexOf('user/login') !== -1 && body) {
        try {
          var data = JSON.parse(body);
          if (data.username && data.username.indexOf('0086') === 0) {
            // 将 0086xxxxxxxxx 改为 86xxxxxxxxx（去掉国际拨号前缀 00）
            data.username = data.username.substring(2);
            body = JSON.stringify(data);
          }
        } catch(e) {}
      }
      return origSend.call(this, body);
    };
    var origFetch = window.fetch;
    if (origFetch) {
      window.fetch = function(url, options) {
        if (url && url.toString().indexOf('user/login') !== -1 && options && options.body) {
          try {
            var data = JSON.parse(options.body);
            if (data.username && data.username.indexOf('0086') === 0) {
              data.username = data.username.substring(2);
              options = Object.assign({}, options, { body: JSON.stringify(data) });
            }
          } catch(e) {}
        }
        return origFetch.apply(this, arguments);
      };
    }
  })();



  // ===== 注入 CSS =====
  function injectCSS() {
    if (document.getElementById('imim-adaptive-css')) return;
    const style = document.createElement('style');
    style.id = 'imim-adaptive-css';
    style.textContent = `
/* ============================================================
   登录页 - 全平台居中美化
   ============================================================ */
.wk-login {
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  min-height: 100vh !important;
  width: 100% !important;
  background: linear-gradient(135deg, #f0f4ff 0%, #e8f0fe 50%, #f5f0ff 100%) !important;
}
body[theme-mode=dark] .wk-login {
  background: linear-gradient(135deg, #0f172a 0%, #1e1b4b 50%, #0f172a 100%) !important;
}
.wk-login-content {
  display: flex !important;
  flex-direction: column !important;
  align-items: center !important;
  justify-content: center !important;
  width: 100% !important;
  max-width: 420px !important;
  padding: 40px 24px !important;
  margin: 0 auto !important;
  height: auto !important;
  min-height: unset !important;
}
.wk-login-content-phonelogin {
  display: flex !important;
  flex-direction: column !important;
  align-items: center !important;
  width: 100% !important;
  height: auto !important;
  background: rgba(255,255,255,0.85) !important;
  border-radius: 20px !important;
  padding: 36px 32px 28px !important;
  box-shadow: 0 8px 32px rgba(22,119,255,0.10), 0 1.5px 6px rgba(0,0,0,0.06) !important;
  backdrop-filter: blur(12px) !important;
}
body[theme-mode=dark] .wk-login-content-phonelogin {
  background: rgba(30,30,50,0.92) !important;
  box-shadow: 0 8px 32px rgba(0,0,0,0.4) !important;
}
.wk-login-content-logo {
  width: 100px !important;
  height: 100px !important;
  max-width: 100px !important;
  max-height: 100px !important;
  margin: 0 auto 16px !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  overflow: hidden !important;
}
.wk-login-content-logo img {
  width: 100% !important;
  height: 100% !important;
  object-fit: contain !important;
  border-radius: 20px !important;
}
.wk-login-content-slogan {
  text-align: center !important;
  font-size: 20px !important;
  font-weight: 600 !important;
  color: #1e293b !important;
  margin-bottom: 28px !important;
  width: 100% !important;
  letter-spacing: 0.5px !important;
}
body[theme-mode=dark] .wk-login-content-slogan {
  color: #e2e8f0 !important;
}
.wk-login-content-form {
  width: 100% !important;
}
/* 登录页输入框样式 */
.wk-login-content-form input,
.wk-login-content-form .ant-input {
  height: 48px !important;
  border-radius: 10px !important;
  border: 1.5px solid #e2e8f0 !important;
  font-size: 15px !important;
  padding: 0 16px !important;
  width: 100% !important;
  margin-bottom: 12px !important;
  background: #f8faff !important;
  transition: border-color 0.2s !important;
}
.wk-login-content-form input:focus,
.wk-login-content-form .ant-input:focus {
  border-color: #1677ff !important;
  outline: none !important;
  box-shadow: 0 0 0 3px rgba(22,119,255,0.1) !important;
}
/* 登录按钮 - 蓝色实心 */
.wk-login-content-form button[type=submit],
.wk-login-content-form .ant-btn-primary,
#imim-login-btn {
  width: 100% !important;
  height: 52px !important;
  background: #1677ff !important;
  border: none !important;
  border-radius: 12px !important;
  color: #fff !important;
  font-size: 17px !important;
  font-weight: 600 !important;
  cursor: pointer !important;
  margin-top: 8px !important;
  margin-bottom: 12px !important;
  letter-spacing: 1px !important;
  box-shadow: 0 4px 16px rgba(22,119,255,0.25) !important;
  transition: all 0.2s !important;
}
.wk-login-content-form button[type=submit]:hover,
.wk-login-content-form .ant-btn-primary:hover,
#imim-login-btn:hover {
  background: #0958d9 !important;
  box-shadow: 0 6px 20px rgba(22,119,255,0.35) !important;
}
/* 注册按钮 - 蓝色描边空心 */
#imim-register-btn {
  width: 100% !important;
  height: 52px !important;
  background: transparent !important;
  border: 2px solid #1677ff !important;
  border-radius: 12px !important;
  color: #1677ff !important;
  font-size: 17px !important;
  font-weight: 600 !important;
  cursor: pointer !important;
  margin-bottom: 16px !important;
  letter-spacing: 1px !important;
  transition: all 0.2s !important;
}
#imim-register-btn:hover {
  background: rgba(22,119,255,0.06) !important;
}
/* 二维码图标按钮 */
#imim-qr-btn {
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  gap: 6px !important;
  width: 100% !important;
  height: 40px !important;
  background: transparent !important;
  border: none !important;
  color: #64748b !important;
  font-size: 14px !important;
  cursor: pointer !important;
  margin-top: 4px !important;
  border-radius: 8px !important;
  transition: color 0.2s, background 0.2s !important;
}
#imim-qr-btn:hover {
  color: #1677ff !important;
  background: rgba(22,119,255,0.06) !important;
}
/* 隐藏原始扫描登录文字和说明区域 */
.wk-login-content-form-scanlogin {
  display: none !important;
}
.wk-login-content-scanlogin {
  display: none !important;
}
/* 二维码登录覆盖层 */
#imim-qr-overlay {
  position: fixed !important;
  top: 0 !important; left: 0 !important;
  width: 100vw !important; height: 100vh !important;
  background: linear-gradient(135deg, #f0f4ff 0%, #e8f0fe 50%, #f5f0ff 100%) !important;
  z-index: 99998 !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
}
body[theme-mode=dark] #imim-qr-overlay {
  background: linear-gradient(135deg, #0f172a 0%, #1e1b4b 50%, #0f172a 100%) !important;
}
#imim-qr-overlay-inner {
  background: rgba(255,255,255,0.92) !important;
  border-radius: 20px !important;
  padding: 40px 32px 32px !important;
  box-shadow: 0 8px 32px rgba(22,119,255,0.12) !important;
  display: flex !important;
  flex-direction: column !important;
  align-items: center !important;
  width: 320px !important;
  max-width: 90vw !important;
}
body[theme-mode=dark] #imim-qr-overlay-inner {
  background: rgba(30,30,50,0.95) !important;
}
#imim-qr-overlay-title {
  font-size: 18px !important;
  font-weight: 600 !important;
  color: #1e293b !important;
  margin-bottom: 6px !important;
}
body[theme-mode=dark] #imim-qr-overlay-title {
  color: #e2e8f0 !important;
}
#imim-qr-overlay-sub {
  font-size: 13px !important;
  color: #64748b !important;
  margin-bottom: 24px !important;
  text-align: center !important;
}
#imim-qr-overlay-qr {
  width: 180px !important;
  height: 180px !important;
  border-radius: 12px !important;
  border: 1.5px solid #e2e8f0 !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  background: #fff !important;
  margin-bottom: 20px !important;
  overflow: hidden !important;
}
#imim-qr-overlay-qr img {
  width: 160px !important;
  height: 160px !important;
}
#imim-qr-overlay-hint {
  font-size: 13px !important;
  color: #64748b !important;
  text-align: center !important;
  margin-bottom: 20px !important;
  line-height: 1.6 !important;
}
#imim-qr-overlay-back {
  width: 100% !important;
  height: 44px !important;
  background: transparent !important;
  border: 1.5px solid #e2e8f0 !important;
  border-radius: 10px !important;
  color: #64748b !important;
  font-size: 15px !important;
  cursor: pointer !important;
  transition: all 0.2s !important;
}
#imim-qr-overlay-back:hover {
  border-color: #1677ff !important;
  color: #1677ff !important;
}
/* ============================================================
   注册弹窗
   ============================================================ */
#imim-reg-overlay {
  position: fixed !important;
  top: 0 !important; left: 0 !important;
  width: 100vw !important; height: 100vh !important;
  background: rgba(0,0,0,0.45) !important;
  z-index: 99999 !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  backdrop-filter: blur(4px) !important;
}
#imim-reg-box {
  background: #fff !important;
  border-radius: 20px !important;
  padding: 32px 28px 24px !important;
  box-shadow: 0 12px 40px rgba(22,119,255,0.18) !important;
  display: flex !important;
  flex-direction: column !important;
  align-items: stretch !important;
  width: 360px !important;
  max-width: 92vw !important;
  max-height: 92vh !important;
  overflow-y: auto !important;
  position: relative !important;
}
body[theme-mode=dark] #imim-reg-box {
  background: #1e293b !important;
}
#imim-reg-title {
  font-size: 20px !important;
  font-weight: 700 !important;
  color: #1e293b !important;
  text-align: center !important;
  margin-bottom: 6px !important;
}
body[theme-mode=dark] #imim-reg-title { color: #e2e8f0 !important; }
#imim-reg-sub {
  font-size: 13px !important;
  color: #64748b !important;
  text-align: center !important;
  margin-bottom: 22px !important;
}
#imim-reg-close {
  position: absolute !important;
  top: 14px !important; right: 16px !important;
  width: 28px !important; height: 28px !important;
  background: none !important;
  border: none !important;
  cursor: pointer !important;
  color: #94a3b8 !important;
  font-size: 20px !important;
  line-height: 1 !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  border-radius: 50% !important;
  transition: background 0.2s !important;
}
#imim-reg-close:hover { background: #f1f5f9 !important; color: #475569 !important; }
.imim-reg-field {
  display: flex !important;
  flex-direction: column !important;
  margin-bottom: 14px !important;
}
.imim-reg-label {
  font-size: 13px !important;
  color: #475569 !important;
  margin-bottom: 5px !important;
  font-weight: 500 !important;
}
body[theme-mode=dark] .imim-reg-label { color: #94a3b8 !important; }
.imim-reg-input-row {
  display: flex !important;
  gap: 8px !important;
  align-items: center !important;
}
.imim-reg-input {
  flex: 1 !important;
  height: 46px !important;
  border: 1.5px solid #e2e8f0 !important;
  border-radius: 10px !important;
  padding: 0 14px !important;
  font-size: 15px !important;
  background: #f8faff !important;
  color: #1e293b !important;
  outline: none !important;
  transition: border-color 0.2s, box-shadow 0.2s !important;
  box-sizing: border-box !important;
  -webkit-appearance: none !important;
}
.imim-reg-input:focus {
  border-color: #1677ff !important;
  box-shadow: 0 0 0 3px rgba(22,119,255,0.10) !important;
}
body[theme-mode=dark] .imim-reg-input {
  background: #0f172a !important;
  border-color: #334155 !important;
  color: #e2e8f0 !important;
}
#imim-reg-sms-btn {
  flex-shrink: 0 !important;
  height: 46px !important;
  padding: 0 14px !important;
  background: #1677ff !important;
  color: #fff !important;
  border: none !important;
  border-radius: 10px !important;
  font-size: 14px !important;
  font-weight: 600 !important;
  cursor: pointer !important;
  white-space: nowrap !important;
  transition: background 0.2s !important;
  min-width: 96px !important;
}
#imim-reg-sms-btn:disabled {
  background: #94a3b8 !important;
  cursor: not-allowed !important;
}
#imim-reg-pwd-hint {
  font-size: 12px !important;
  color: #94a3b8 !important;
  margin-top: 4px !important;
  line-height: 1.5 !important;
}
#imim-reg-error {
  font-size: 13px !important;
  color: #ef4444 !important;
  text-align: center !important;
  min-height: 18px !important;
  margin-bottom: 8px !important;
}
#imim-reg-success {
  font-size: 14px !important;
  color: #22c55e !important;
  text-align: center !important;
  min-height: 18px !important;
  margin-bottom: 8px !important;
}
#imim-reg-submit {
  width: 100% !important;
  height: 52px !important;
  background: #1677ff !important;
  border: none !important;
  border-radius: 12px !important;
  color: #fff !important;
  font-size: 17px !important;
  font-weight: 600 !important;
  cursor: pointer !important;
  letter-spacing: 1px !important;
  box-shadow: 0 4px 16px rgba(22,119,255,0.25) !important;
  transition: all 0.2s !important;
  margin-top: 4px !important;
}
#imim-reg-submit:hover { background: #0958d9 !important; }
#imim-reg-submit:disabled { background: #94a3b8 !important; cursor: not-allowed !important; box-shadow: none !important; }
#imim-reg-back-login {
  text-align: center !important;
  margin-top: 14px !important;
  font-size: 13px !important;
  color: #64748b !important;
}
#imim-reg-back-login a {
  color: #1677ff !important;
  text-decoration: none !important;
  font-weight: 500 !important;
  cursor: pointer !important;
}

/* ============================================================
   桌面端布局（宽屏）
   ============================================================ */
@media screen and (min-width: 769px) {
  /* 整体布局：左侧图标栏 + 内容区 */
  .wk-layout {
    display: flex !important;
    flex-direction: row !important;
    height: 100vh !important;
    width: 100vw !important;
    overflow: hidden !important;
    position: fixed !important;
    top: 0 !important;
    left: 0 !important;
  }
  /* 左侧图标导航栏 */
  .wk-layout-tab {
    width: 64px !important;
    min-width: 64px !important;
    max-width: 64px !important;
    height: 100vh !important;
    flex-shrink: 0 !important;
    overflow: hidden !important;
    display: flex !important;
    flex-direction: column !important;
  }
  /* 内容区 */
  .wk-layout-content {
    flex: 1 !important;
    display: flex !important;
    flex-direction: row !important;
    height: 100vh !important;
    overflow: hidden !important;
    min-width: 0 !important;
  }
  /* 左栏（会话列表） */
  .wk-layout-content-left {
    width: 280px !important;
    min-width: 280px !important;
    max-width: 280px !important;
    height: 100vh !important;
    overflow-y: auto !important;
    flex-shrink: 0 !important;
  }
  /* 右栏（聊天窗口） */
  .wk-layout-content-right {
    flex: 1 !important;
    height: 100vh !important;
    overflow: hidden !important;
    min-width: 0 !important;
  }
}

/* ============================================================
   平板端布局
   ============================================================ */
@media screen and (min-width: 769px) and (max-width: 1024px) {
  .wk-layout-tab {
    width: 56px !important;
    min-width: 56px !important;
  }
  .wk-layout-content-left {
    width: 240px !important;
    min-width: 240px !important;
  }
}

/* ============================================================
   移动端布局（手机）
   ============================================================ */
@media screen and (max-width: 768px) {
  * { box-sizing: border-box; -webkit-tap-highlight-color: transparent; }

  body {
    background: #ededed !important;
    font-family: -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Helvetica Neue', sans-serif !important;
    overflow: hidden !important;
  }

  /* 根容器 */
  #root {
    width: 100vw !important;
    height: 100vh !important;
    overflow: hidden !important;
    position: fixed !important;
    top: 0 !important;
    left: 0 !important;
  }

  /* 基础容器 */
  .wk-base {
    width: 100vw !important;
    height: 100vh !important;
    overflow: hidden !important;
    position: relative !important;
  }

  /* 整体布局 */
  .wk-layout {
    display: flex !important;
    flex-direction: column !important;
    width: 100vw !important;
    height: 100vh !important;
    overflow: hidden !important;
    position: relative !important;
  }

  /* 隐藏左侧图标导航栏 */
  .wk-layout-tab {
    display: none !important;
  }

  /* 内容区占满剩余高度（减去底部导航栏 56px） */
  .wk-layout-content {
    flex: 1 !important;
    display: flex !important;
    flex-direction: column !important;
    width: 100vw !important;
    height: calc(100vh - 56px) !important;
    overflow: hidden !important;
    position: relative !important;
  }

  /* 左栏（会话列表）全宽 */
  .wk-layout-content-left {
    position: absolute !important;
    top: 0 !important;
    left: 0 !important;
    width: 100vw !important;
    height: 100% !important;
    overflow-y: auto !important;
    -webkit-overflow-scrolling: touch !important;
    z-index: 1 !important;
    background: #ededed !important;
  }

  /* 右栏（聊天窗口）从右侧滑入 */
  .wk-layout-content-right {
    position: fixed !important;
    top: 0 !important;
    left: 0 !important;
    width: 100vw !important;
    height: 100vh !important;
    z-index: 200 !important;
    transform: translateX(100%) !important;
    transition: transform 0.28s cubic-bezier(0.4, 0, 0.2, 1) !important;
    background: #fff !important;
    overflow: hidden !important;
  }

  /* 聊天窗口打开状态 */
  .wk-layout-content-right.wk-chat-open {
    transform: translateX(0) !important;
  }

  /* 底部导航栏 */
  .wk-mobile-tabbar {
    position: fixed !important;
    bottom: 0 !important;
    left: 0 !important;
    right: 0 !important;
    height: 56px !important;
    background: #f7f7f7 !important;
    border-top: 0.5px solid #d9d9d9 !important;
    display: flex !important;
    align-items: center !important;
    justify-content: space-around !important;
    z-index: 9999 !important;
    padding-bottom: env(safe-area-inset-bottom, 0px) !important;
  }

  .wk-mobile-tabbar-item {
    display: flex !important;
    flex-direction: column !important;
    align-items: center !important;
    justify-content: center !important;
    flex: 1 !important;
    height: 100% !important;
    cursor: pointer !important;
    color: #888 !important;
    font-size: 10px !important;
    gap: 2px !important;
    position: relative !important;
    user-select: none !important;
    -webkit-user-select: none !important;
  }

  .wk-mobile-tabbar-item.active {
    color: #07c160 !important;
  }

  .wk-mobile-tabbar-item svg {
    width: 24px !important;
    height: 24px !important;
  }

  .wk-mobile-tabbar-badge {
    position: absolute !important;
    top: 4px !important;
    right: calc(50% - 18px) !important;
    background: #f00 !important;
    color: #fff !important;
    border-radius: 10px !important;
    font-size: 10px !important;
    min-width: 16px !important;
    height: 16px !important;
    line-height: 16px !important;
    text-align: center !important;
    padding: 0 4px !important;
  }

  /* 隐藏桌面端侧边栏设置菜单 */
  .wk-main-sider {
    display: none !important;
  }
  .wk-sider-setting-list {
    display: none !important;
  }

  /* ============================================================
     右键菜单修复：移动端改为底部弹出式菜单
     ============================================================ */
  .wk-contextmenus {
    position: fixed !important;
    bottom: 56px !important;
    left: 50% !important;
    transform: translateX(-50%) !important;
    top: auto !important;
    width: 90vw !important;
    max-width: 360px !important;
    background: #fff !important;
    border-radius: 16px 16px 0 0 !important;
    box-shadow: 0 -4px 24px rgba(0,0,0,0.12) !important;
    z-index: 10000 !important;
    overflow: hidden !important;
    padding: 8px 0 !important;
  }
  body[theme-mode=dark] .wk-contextmenus {
    background: #2c2c2c !important;
  }
  .wk-contextmenus ul {
    list-style: none !important;
    margin: 0 !important;
    padding: 0 !important;
  }
  .wk-contextmenus ul li {
    padding: 14px 20px !important;
    font-size: 16px !important;
    color: #333 !important;
    border-bottom: 0.5px solid #f0f0f0 !important;
    cursor: pointer !important;
    text-align: center !important;
  }
  body[theme-mode=dark] .wk-contextmenus ul li {
    color: #e0e0e0 !important;
    border-bottom-color: #3a3a3a !important;
  }
  .wk-contextmenus ul li:last-child {
    border-bottom: none !important;
    color: #ff4d4f !important;
  }
  .wk-contextmenus ul li:active {
    background: #f5f5f5 !important;
  }

  /* 会话列表顶部工具栏（搜索、新建按钮）*/
  .wk-chat-header,
  .wk-conversationlist-header,
  [class*="conversation"][class*="header"] {
    position: sticky !important;
    top: 0 !important;
    z-index: 10 !important;
    background: #ededed !important;
    display: flex !important;
    align-items: center !important;
    padding: 8px 12px !important;
    min-height: 48px !important;
  }

  /* 断开连接提示条 */
  [class*="disconnect"],
  [class*="offline"],
  [class*="reconnect"] {
    font-size: 12px !important;
    padding: 4px 12px !important;
    text-align: center !important;
    background: #fffbe6 !important;
    color: #d46b08 !important;
    border-bottom: 1px solid #ffe58f !important;
  }

  /* 会话列表适配 */
  .wk-chat-conversation-list,
  .wk-conversationlist {
    padding-bottom: 0 !important;
    height: 100% !important;
    overflow-y: auto !important;
    -webkit-overflow-scrolling: touch !important;
  }

  /* 会话列表搜索栏 */
  .wk-chat-search {
    position: sticky !important;
    top: 0 !important;
    z-index: 10 !important;
    background: #ededed !important;
  }

  /* 聊天内容区 */
  .wk-chat {
    height: 100% !important;
    display: flex !important;
    flex-direction: column !important;
    overflow: hidden !important;
  }

  .wk-chat-content {
    flex: 1 !important;
    overflow-y: auto !important;
    -webkit-overflow-scrolling: touch !important;
  }

  /* 头像圆角 */
  .wk-avatar {
    border-radius: 8px !important;
  }

  /* 登录页在手机端的额外优化 */
  .wk-login-content-phonelogin {
    padding: 28px 20px 24px !important;
    border-radius: 16px !important;
  }
  .wk-login-content {
    padding: 24px 16px !important;
  }
}

/* ============================================================
   深色模式
   ============================================================ */
body[theme-mode=dark] .wk-layout {
  background: #111 !important;
}
body[theme-mode=dark] .wk-layout-content-left {
  background: #1a1a1a !important;
}
body[theme-mode=dark] .wk-layout-content-right {
  background: #111 !important;
}
body[theme-mode=dark] .wk-mobile-tabbar {
  background: #1a1a1a !important;
  border-top-color: #333 !important;
}
body[theme-mode=dark] .wk-mobile-tabbar-item {
  color: #666 !important;
}
body[theme-mode=dark] .wk-mobile-tabbar-item.active {
  color: #07c160 !important;
}
    `;
    document.head.appendChild(style);
  }

  // ===== 底部导航栏 =====
  var currentTab = 'chat';

  var tabConfig = [
    {
      id: 'chat',
      label: '消息',
      icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>`
    },
    {
      id: 'contacts',
      label: '联系人',
      icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>`
    },
    {
      id: 'discover',
      label: '发现',
      icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polygon points="16.24 7.76 14.12 14.12 7.76 16.24 9.88 9.88 16.24 7.76"/></svg>`
    },
    {
      id: 'me',
      label: '我',
      icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>`
    }
  ];

  // 判断当前是否已登录（主界面已渲染）
  function isLoggedIn() {
    // 登录后会出现 wk-layout 容器，登录页是 wk-login
    var layout = document.querySelector('.wk-layout');
    var loginPage = document.querySelector('.wk-login');
    // 有主界面且没有登录页，才算已登录
    return !!layout && !loginPage;
  }

  function injectTabBar() {
    if (!isMobile()) return;
    // 未登录时：移除已有的 TabBar 并返回
    if (!isLoggedIn()) {
      var existing = document.getElementById('wk-mobile-tabbar');
      if (existing) existing.remove();
      return;
    }
    if (document.getElementById('wk-mobile-tabbar')) return;

    var tabbar = document.createElement('div');
    tabbar.id = 'wk-mobile-tabbar';
    tabbar.className = 'wk-mobile-tabbar';

    tabConfig.forEach(function(tab) {
      var item = document.createElement('div');
      item.className = 'wk-mobile-tabbar-item' + (tab.id === currentTab ? ' active' : '');
      item.dataset.tab = tab.id;
      item.innerHTML = tab.icon + '<span>' + tab.label + '</span>';
      item.addEventListener('click', function() {
        switchTab(tab.id);
      });
      tabbar.appendChild(item);
    });

    document.body.appendChild(tabbar);
  }

  function switchTab(tabId) {
    if (currentTab === tabId) return;
    currentTab = tabId;

    // 更新底部导航栏激活状态
    var items = document.querySelectorAll('.wk-mobile-tabbar-item');
    items.forEach(function(item) {
      if (item.dataset.tab === tabId) {
        item.classList.add('active');
      } else {
        item.classList.remove('active');
      }
    });

    // 根据 tab 切换内容
    var leftPanel = document.querySelector('.wk-layout-content-left');
    var rightPanel = document.querySelector('.wk-layout-content-right');

    // 关闭聊天窗口
    if (rightPanel) {
      rightPanel.classList.remove('wk-chat-open');
    }

    // 根据 tab 触发不同的导航
    if (tabId === 'chat') {
      // 点击会话列表 tab - 触发侧边栏的会话按钮
      triggerSiderItem(0);
    } else if (tabId === 'contacts') {
      // 点击联系人 tab - 触发侧边栏的通讯录按钮
      triggerSiderItem(1);
    } else if (tabId === 'discover') {
      // 发现页（暂时显示空白或触发第三个按钮）
      triggerSiderItem(2);
    } else if (tabId === 'me') {
      // 我的页面 - 触发头像点击
      triggerAvatarClick();
    }
  }

  function triggerSiderItem(index) {
    // 尝试点击 wk-main-sider 中的导航项
    var siderItems = document.querySelectorAll('.wk-main-sider-item');
    if (siderItems && siderItems[index]) {
      siderItems[index].click();
      return;
    }
    // 备选：点击 wk-layout-tab 中的 li 元素
    var tabItems = document.querySelectorAll('.wk-layout-tab li');
    if (tabItems && tabItems[index]) {
      tabItems[index].click();
    }
  }

  function triggerAvatarClick() {
    var avatar = document.querySelector('.wk-main-sider-avatar');
    if (avatar) {
      avatar.click();
    }
  }

  // ===== 监听聊天窗口打开/关闭 =====
  function setupChatObserver() {
    if (!isMobile()) return;

    var rightPanel = document.querySelector('.wk-layout-content-right');
    if (!rightPanel) return;

    // 监听右侧面板内容变化，判断是否有聊天内容
    var observer = new MutationObserver(function() {
      checkChatState();
    });

    observer.observe(rightPanel, {
      childList: true,
      subtree: true,
      attributes: true
    });

    // 监听左侧面板点击，打开聊天
    var leftPanel = document.querySelector('.wk-layout-content-left');
    if (leftPanel) {
      leftPanel.addEventListener('click', function(e) {
        // 点击会话列表项时，延迟检查是否打开了聊天
        setTimeout(function() {
          checkChatState();
        }, 100);
      });
    }
  }

  function checkChatState() {
    if (!isMobile()) return;
    var rightPanel = document.querySelector('.wk-layout-content-right');
    if (!rightPanel) return;

    // 检查右侧面板是否有聊天内容（非空）
    var chatContent = rightPanel.querySelector('.wk-chat, .wk-viewqueue-view');
    var hasContent = chatContent && chatContent.children.length > 0;

    // 也检查 wk-chat-empty 是否存在（空状态）
    var isEmpty = rightPanel.querySelector('.wk-chat-empty');

    if (hasContent && !isEmpty) {
      rightPanel.classList.add('wk-chat-open');
    } else {
      rightPanel.classList.remove('wk-chat-open');
    }
  }

  // ===== 添加返回按钮到聊天页面 =====
  function injectBackButton() {
    if (!isMobile()) return;
    if (document.getElementById('wk-mobile-back-btn')) return;

    var rightPanel = document.querySelector('.wk-layout-content-right');
    if (!rightPanel) return;

    var backBtn = document.createElement('div');
    backBtn.id = 'wk-mobile-back-btn';
    backBtn.style.cssText = `
      position: fixed;
      top: 0;
      left: 0;
      width: 44px;
      height: 44px;
      z-index: 9999;
      display: none;
      align-items: center;
      justify-content: center;
      cursor: pointer;
      color: #07c160;
    `;
    backBtn.innerHTML = `<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></svg>`;
    backBtn.addEventListener('click', function() {
      var rp = document.querySelector('.wk-layout-content-right');
      if (rp) {
        rp.classList.remove('wk-chat-open');
      }
      backBtn.style.display = 'none';
    });

    document.body.appendChild(backBtn);

    // 监听聊天窗口状态变化
    var observer = new MutationObserver(function() {
      var rp = document.querySelector('.wk-layout-content-right');
      if (rp && rp.classList.contains('wk-chat-open')) {
        backBtn.style.display = 'flex';
      } else {
        backBtn.style.display = 'none';
      }
    });

    var rp = document.querySelector('.wk-layout-content-right');
    if (rp) {
      observer.observe(rp, { attributes: true, attributeFilter: ['class'] });
    }
  }

  // ===== 修复 username 格式 =====
  function fixLoginUsername() {
    // 拦截 XMLHttpRequest
    var origOpen = XMLHttpRequest.prototype.open;
    var origSend = XMLHttpRequest.prototype.send;

    XMLHttpRequest.prototype.open = function(method, url) {
      this._url = url;
      this._method = method;
      return origOpen.apply(this, arguments);
    };

    XMLHttpRequest.prototype.send = function(body) {
      if (this._url && this._url.toString().includes('user/login') && body) {
        try {
          var data = JSON.parse(body);
          if (data.username && data.username.startsWith('0086')) {
            // 0086xxxxxxxxx -> 86xxxxxxxxx (去掉前导00)
            data.username = data.username.substring(2);
            body = JSON.stringify(data);
          }
        } catch(e) {}
      }
      return origSend.call(this, body);
    };

    // 拦截 fetch
    var origFetch = window.fetch;
    window.fetch = function(url, options) {
      if (url && url.toString().includes('user/login') && options && options.body) {
        try {
          var data = JSON.parse(options.body);
          if (data.username && data.username.startsWith('0086')) {
            data.username = data.username.substring(2);
            options = Object.assign({}, options, { body: JSON.stringify(data) });
          }
        } catch(e) {}
      }
      return origFetch.apply(this, arguments);
    };
  }

  // ===== 深色/浅色切换按钮 =====
  function injectThemeToggle() {
    if (document.getElementById('imim-theme-btn')) return;
    var btn = document.createElement('button');
    btn.id = 'imim-theme-btn';
    btn.title = '切换深色/浅色模式';
    btn.style.cssText = `
      position: fixed;
      z-index: 99999;
      width: 44px;
      height: 44px;
      border-radius: 50%;
      border: none;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;
      box-shadow: 0 2px 12px rgba(0,0,0,0.15);
      transition: all 0.3s ease;
      padding: 0;
    `;

    function updatePosition() {
      if (isMobile()) {
        btn.style.top = '14px';
        btn.style.right = '14px';
        btn.style.bottom = 'auto';
        btn.style.left = 'auto';
      } else {
        btn.style.bottom = '24px';
        btn.style.left = '24px';
        btn.style.top = 'auto';
        btn.style.right = 'auto';
      }
    }

    function updateIcon() {
      var isDark = document.body.getAttribute('theme-mode') === 'dark';
      if (isDark) {
        btn.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#e2e8f0" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>`;
        btn.style.background = '#1e293b';
      } else {
        btn.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#f59e0b" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>`;
        btn.style.background = '#ffffff';
      }
    }

    btn.addEventListener('click', function() {
      var isDark = document.body.getAttribute('theme-mode') === 'dark';
      if (isDark) {
        document.body.removeAttribute('theme-mode');
        localStorage.setItem('theme-mode', '0');
      } else {
        document.body.setAttribute('theme-mode', 'dark');
        localStorage.setItem('theme-mode', '1');
      }
      updateIcon();
    });

    updateIcon();
    updatePosition();
    window.addEventListener('resize', updatePosition);
    document.body.appendChild(btn);
  }

  // ===== 工具函数 =====
  function isMobile() {
    return window.innerWidth <= 768;
  }

  // ===== 移动端主初始化 =====
  function initMobile() {
    if (!isMobile()) return;
    injectTabBar();
    if (isLoggedIn()) {
      setupChatObserver();
      injectBackButton();
      setupContextMenuFix();
    }

    // 监听 DOM 变化，确保聊天窗口状态正确
    var observer = new MutationObserver(function() {
      checkChatState();
      injectTabBar(); // 内部已含登录状态检测
      if (isLoggedIn()) {
        injectBackButton();
        setupContextMenuFix();
      }
    });

    var root = document.getElementById('root');
    if (root) {
      observer.observe(root, { childList: true, subtree: true });
    }
  }

  // ===== 移动端右键菜单修复 =====
  var _contextMenuFixInstalled = false;
  function setupContextMenuFix() {
    if (_contextMenuFixInstalled) return;
    _contextMenuFixInstalled = true;

    // 点击空白处关闭菜单
    document.addEventListener('click', function(e) {
      var menu = document.querySelector('.wk-contextmenus');
      if (!menu) return;
      // 如果点击的不是菜单内容，则关闭菜单
      if (!menu.contains(e.target)) {
        // 触发点击菜单外部来关闭（模拟 ESC）
        document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', keyCode: 27, bubbles: true}));
        // 备用：直接隐藏
        setTimeout(function() {
          var m = document.querySelector('.wk-contextmenus');
          if (m) m.style.display = 'none';
        }, 100);
      }
    }, true);

    // 监听菜单出现，添加遇层背景
    var menuObserver = new MutationObserver(function() {
      var menu = document.querySelector('.wk-contextmenus');
      var overlay = document.getElementById('imim-menu-overlay');
      if (menu && window.getComputedStyle(menu).display !== 'none') {
        if (!overlay) {
          overlay = document.createElement('div');
          overlay.id = 'imim-menu-overlay';
          overlay.style.cssText = 'position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;background:rgba(0,0,0,0.3);';
          overlay.addEventListener('click', function() {
            document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', keyCode: 27, bubbles: true}));
            setTimeout(function() {
              var m = document.querySelector('.wk-contextmenus');
              if (m) m.style.display = 'none';
              var ov = document.getElementById('imim-menu-overlay');
              if (ov) ov.remove();
            }, 100);
          });
          document.body.appendChild(overlay);
        }
      } else {
        if (overlay) overlay.remove();
      }
    });

    var root = document.getElementById('root');
    if (root) {
      menuObserver.observe(root, {childList: true, subtree: true, attributes: true});
    }
  }

  // ===== 注册弹窗 =====
  function showRegisterOverlay() {
    if (document.getElementById('imim-reg-overlay')) return;

    var overlay = document.createElement('div');
    overlay.id = 'imim-reg-overlay';
    overlay.innerHTML = `
      <div id="imim-reg-box">
        <button id="imim-reg-close" title="关闭">&#x2715;</button>
        <div id="imim-reg-title">注册账号</div>
        <div id="imim-reg-sub">注册 imimchat，开始安全加密通讯</div>

        <div class="imim-reg-field">
          <div class="imim-reg-label">手机号（中国大陆）</div>
          <div class="imim-reg-input-row">
            <input id="imim-reg-phone" class="imim-reg-input" type="tel" maxlength="11"
              placeholder="请输入 11 位手机号" autocomplete="tel" inputmode="numeric" />
          </div>
        </div>

        <div class="imim-reg-field">
          <div class="imim-reg-label">验证码</div>
          <div class="imim-reg-input-row">
            <input id="imim-reg-code" class="imim-reg-input" type="text" maxlength="6"
              placeholder="6 位数字验证码" autocomplete="one-time-code" inputmode="numeric" />
            <button id="imim-reg-sms-btn" type="button">获取验证码</button>
          </div>
        </div>

        <div class="imim-reg-field">
          <div class="imim-reg-label">昵称</div>
          <input id="imim-reg-name" class="imim-reg-input" type="text" maxlength="20"
            placeholder="2-20 个字符" autocomplete="nickname" />
        </div>

        <div class="imim-reg-field">
          <div class="imim-reg-label">密码</div>
          <input id="imim-reg-pwd" class="imim-reg-input" type="password" maxlength="32"
            placeholder="8-32 位，含大小写字母和数字" autocomplete="new-password" />
          <div id="imim-reg-pwd-hint">密码须包含大写字母、小写字母和数字，长度 8-32 位</div>
        </div>

        <div class="imim-reg-field">
          <div class="imim-reg-label">确认密码</div>
          <input id="imim-reg-pwd2" class="imim-reg-input" type="password" maxlength="32"
            placeholder="再次输入密码" autocomplete="new-password" />
        </div>

        <div id="imim-reg-error"></div>
        <div id="imim-reg-success"></div>

        <button id="imim-reg-submit" type="button">注 册</button>
        <div id="imim-reg-back-login">已有账号？<a id="imim-reg-back-a">返回登录</a></div>
      </div>
    `;

    document.body.appendChild(overlay);

    // 关闭按钮
    document.getElementById('imim-reg-close').addEventListener('click', function() {
      overlay.remove();
    });
    document.getElementById('imim-reg-back-a').addEventListener('click', function() {
      overlay.remove();
    });
    // 点击背景关闭
    overlay.addEventListener('click', function(e) {
      if (e.target === overlay) overlay.remove();
    });

    // 验证码倒计时
    var smsBtn = document.getElementById('imim-reg-sms-btn');
    var countdownTimer = null;
    smsBtn.addEventListener('click', function() {
      var phone = document.getElementById('imim-reg-phone').value.trim();
      if (!/^1[3-9]\d{9}$/.test(phone)) {
        setRegError('请输入正确的 11 位手机号');
        return;
      }
      smsBtn.disabled = true;
      smsBtn.textContent = '发送中...';
      setRegError('');
      fetch('/register/sms', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({phone: phone})
      }).then(function(r) { return r.json(); }).then(function(d) {
        if (d.code === 200) {
          setRegSuccess(d.msg || '验证码已发送');
          var sec = 60;
          smsBtn.textContent = sec + 's';
          countdownTimer = setInterval(function() {
            sec--;
            if (sec <= 0) {
              clearInterval(countdownTimer);
              smsBtn.disabled = false;
              smsBtn.textContent = '重新获取';
            } else {
              smsBtn.textContent = sec + 's';
            }
          }, 1000);
        } else {
          smsBtn.disabled = false;
          smsBtn.textContent = '获取验证码';
          setRegError(d.msg || '发送失败，请重试');
        }
      }).catch(function() {
        smsBtn.disabled = false;
        smsBtn.textContent = '获取验证码';
        setRegError('网络错误，请检查连接后重试');
      });
    });

    // 提交注册
    document.getElementById('imim-reg-submit').addEventListener('click', function() {
      var phone = document.getElementById('imim-reg-phone').value.trim();
      var code  = document.getElementById('imim-reg-code').value.trim();
      var name  = document.getElementById('imim-reg-name').value.trim();
      var pwd   = document.getElementById('imim-reg-pwd').value;
      var pwd2  = document.getElementById('imim-reg-pwd2').value;

      if (!/^1[3-9]\d{9}$/.test(phone)) { setRegError('请输入正确的 11 位手机号'); return; }
      if (!/^\d{6}$/.test(code))         { setRegError('请输入 6 位数字验证码'); return; }
      if (!name || name.length < 1 || name.length > 20) { setRegError('昵称长度须为 1-20 个字符'); return; }
      if (pwd.length < 8 || pwd.length > 32) { setRegError('密码长度须为 8-32 位'); return; }
      if (!/[A-Z]/.test(pwd)) { setRegError('密码须包含至少一个大写字母'); return; }
      if (!/[a-z]/.test(pwd)) { setRegError('密码须包含至少一个小写字母'); return; }
      if (!/\d/.test(pwd))    { setRegError('密码须包含至少一个数字'); return; }
      if (pwd !== pwd2)        { setRegError('两次输入的密码不一致'); return; }

      setRegError('');
      var submitBtn = document.getElementById('imim-reg-submit');
      submitBtn.disabled = true;
      submitBtn.textContent = '注册中...';

      fetch('/register/submit', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({phone: phone, code: code, name: name, password: pwd})
      }).then(function(r) { return r.json(); }).then(function(d) {
        submitBtn.disabled = false;
        submitBtn.textContent = '注 册';
        if (d.code === 200) {
          setRegSuccess('🎉 注册成功！正在跳转登录...');
          // 3 秒后关闭弹窗并自动填入手机号
          setTimeout(function() {
            overlay.remove();
            // 尝试自动填入手机号到登录表单
            var phoneInput = document.querySelector('.wk-login-content-form input[type="text"], .wk-login-content-form input[type="tel"], .wk-login-content-form input:first-of-type');
            if (phoneInput) {
              phoneInput.focus();
              var nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
              nativeInputValueSetter.call(phoneInput, phone);
              phoneInput.dispatchEvent(new Event('input', {bubbles: true}));
            }
          }, 2000);
        } else {
          setRegError(d.msg || '注册失败，请重试');
        }
      }).catch(function() {
        submitBtn.disabled = false;
        submitBtn.textContent = '注 册';
        setRegError('网络错误，请检查连接后重试');
      });
    });
  }

  function setRegError(msg) {
    var el = document.getElementById('imim-reg-error');
    var el2 = document.getElementById('imim-reg-success');
    if (el) el.textContent = msg;
    if (el2 && msg) el2.textContent = '';
  }
  function setRegSuccess(msg) {
    var el = document.getElementById('imim-reg-success');
    var el2 = document.getElementById('imim-reg-error');
    if (el) el.textContent = msg;
    if (el2 && msg) el2.textContent = '';
  }

  // ===== 登录页 UI 注入（蓝色登录按钮 + 注册按钮 + 二维码图标）=====
  function injectLoginUI() {
    // 找到登录表单容器
    var form = document.querySelector('.wk-login-content-form');
    if (!form) return;
    // 避免重复注入
    if (document.getElementById('imim-register-btn')) return;

    // 找到原始登录按钮，覆盖其样式
    var origBtn = form.querySelector('button');
    if (origBtn) {
      origBtn.style.cssText = 'width:100%!important;height:52px!important;background:#1677ff!important;border:none!important;border-radius:12px!important;color:#fff!important;font-size:17px!important;font-weight:600!important;cursor:pointer!important;margin-top:8px!important;margin-bottom:12px!important;letter-spacing:1px!important;box-shadow:0 4px 16px rgba(22,119,255,0.25)!important;transition:all 0.2s!important;display:block!important;';
    }

    // 注册「注册」按钮
    var regBtn = document.createElement('button');
    regBtn.id = 'imim-register-btn';
    regBtn.type = 'button';
    regBtn.textContent = '注册';
    regBtn.addEventListener('click', function() {
      showRegisterOverlay();
    });
    form.appendChild(regBtn);

    // 注入「扫码登录」图标按钮
    var qrBtn = document.createElement('button');
    qrBtn.id = 'imim-qr-btn';
    qrBtn.type = 'button';
    qrBtn.innerHTML = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="3" height="3"/><rect x="18" y="14" width="3" height="3"/><rect x="14" y="18" width="3" height="3"/><rect x="18" y="18" width="3" height="3"/></svg><span>扫码登录</span>';
    qrBtn.addEventListener('click', function() {
      showQROverlay();
    });
    form.appendChild(qrBtn);

    // 隐藏原始的「扫描登录」文字链接（如果存在）
    var scanLinks = document.querySelectorAll('.wk-login-content-form a, .wk-login-content a');
    scanLinks.forEach(function(el) {
      if (el.textContent.indexOf('扫描') !== -1 || el.textContent.indexOf('二维码') !== -1) {
        el.style.display = 'none';
      }
    });
  }

  // 展示二维码登录覆盖层
  function showQROverlay() {
    if (document.getElementById('imim-qr-overlay')) return;

    // 获取原始的二维码登录内容
    var origQRSection = document.querySelector('.wk-login-content-qrcode, [class*="qrcode"], [class*="qr-code"]');

    var overlay = document.createElement('div');
    overlay.id = 'imim-qr-overlay';
    overlay.innerHTML = `
      <div id="imim-qr-overlay-inner">
        <div id="imim-qr-overlay-title">扫码登录</div>
        <div id="imim-qr-overlay-sub">使用手机 imimchat 扫码登录</div>
        <div id="imim-qr-overlay-qr">
          ${origQRSection ? origQRSection.innerHTML : '<div style="width:160px;height:160px;display:flex;align-items:center;justify-content:center;flex-direction:column;color:#94a3b8;font-size:13px;gap:12px;"><svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="#1677ff" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="3" height="3"/><rect x="18" y="14" width="3" height="3"/><rect x="14" y="18" width="3" height="3"/><rect x="18" y="18" width="3" height="3"/></svg><span>二维码加载中...</span></div>'}
        </div>
        <div id="imim-qr-overlay-hint">在手机上打开 imimchat<br>进入 <b>消息</b> &gt; <b>+</b> &gt; <b>扫一扫</b><br>将摄像头对准二维码扫描登录</div>
        <button id="imim-qr-overlay-back" onclick="document.getElementById('imim-qr-overlay').remove()">返回手机号登录</button>
      </div>
    `;

    // 如果原始页面有真实二维码，尝试触发它生成
    var origScanBtn = document.querySelector('.wk-login-content a[class*="scan"], .wk-login-content button[class*="scan"]');
    if (!origScanBtn) {
      // 找到原始扫描登录按钮并触发
      var allEls = document.querySelectorAll('.wk-login-content button, .wk-login-content a, .wk-login-content span');
      for (var i = 0; i < allEls.length; i++) {
        var txt = allEls[i].textContent.trim();
        if (txt === '扫描登录' || txt === '二维码登录') {
          origScanBtn = allEls[i];
          break;
        }
      }
    }

    document.body.appendChild(overlay);

    // 触发原始扫码登录流程，等待二维码生成
    if (origScanBtn) {
      origScanBtn.click();
      // 等待二维码生成后将其内容复制到覆盖层
      setTimeout(function() {
        var qrImg = document.querySelector('.wk-login-content canvas, .wk-login-content img[src*="qr"], .wk-login-content [class*="qrcode"] canvas, .wk-login-content [class*="qrcode"] img');
        var qrContainer = document.getElementById('imim-qr-overlay-qr');
        if (qrImg && qrContainer) {
          qrContainer.innerHTML = '';
          var clone = qrImg.cloneNode(true);
          clone.style.cssText = 'width:160px!important;height:160px!important;';
          qrContainer.appendChild(clone);
        }
      }, 1500);
    }
  }

  // ===== 主初始化 =====
  function init() {
    // 恢复主题
    if (localStorage.getItem('theme-mode') === '1') {
      document.body.setAttribute('theme-mode', 'dark');
    }

    injectCSS();
    fixLoginUsername();
    injectThemeToggle();

    // 登录页 UI 注入（多次尝试确保 React 渲染完成）
    setTimeout(injectLoginUI, 300);
    setTimeout(injectLoginUI, 800);
    setTimeout(injectLoginUI, 2000);

    // 用 MutationObserver 监听登录页出现
    var loginObserver = new MutationObserver(function() {
      if (document.querySelector('.wk-login-content-form') && !document.getElementById('imim-register-btn')) {
        injectLoginUI();
      }
    });
    loginObserver.observe(document.body, { childList: true, subtree: true });

    // 等待 React 渲染后初始化移动端
    setTimeout(initMobile, 500);
    setTimeout(initMobile, 1500);
    setTimeout(initMobile, 3000);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }

  // 窗口大小变化时重新初始化
  window.addEventListener('resize', function() {
    if (isMobile()) {
      initMobile();
    } else {
      // 桌面端：移除移动端元素
      var tabbar = document.getElementById('wk-mobile-tabbar');
      if (tabbar) tabbar.style.display = 'none';
      var backBtn = document.getElementById('wk-mobile-back-btn');
      if (backBtn) backBtn.style.display = 'none';
    }
  });

})();
