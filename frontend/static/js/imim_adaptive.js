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

  // ===== 立即执行：修复 username 格式（必须在 main.js 加载前执行）=====
  (function() {
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
    }

    // 监听 DOM 变化，确保聊天窗口状态正确
    var observer = new MutationObserver(function() {
      checkChatState();
      injectTabBar(); // 内部已含登录状态检测
      if (isLoggedIn()) injectBackButton();
    });

    var root = document.getElementById('root');
    if (root) {
      observer.observe(root, { childList: true, subtree: true });
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
