/**
 * imimchat TRTC 视频通话模块 v1
 * 功能:
 *   1. 聊天页视频通话按钮（动态注入到聊天窗口顶部）
 *   2. TRTC SDK 动态加载
 *   3. 视频通话界面（全屏覆盖层）
 *   4. UserSig 获取
 *   5. 通话控制（静音/摄像头/挂断）
 */
(function() {
  'use strict';

  // ===== 配置 =====
  var TRTC_API = 'https://wed.imim.chat/trtc/usersig';
  var TRTC_SDK_URL = 'https://web.sdk.qcloud.com/trtc/webrtc/v5/dist/trtc.js';

  // ===== 状态 =====
  var _trtcClient = null;
  var _inCall = false;
  var _callRoomId = 0;
  var _callUserId = '';
  var _localStream = null;
  var _sdkLoaded = false;
  var _sdkLoading = false;
  var _sdkLoadCallbacks = [];

  // ===== SDK 动态加载 =====
  function loadTRTCSDK(callback) {
    if (_sdkLoaded) {
      callback(null);
      return;
    }
    _sdkLoadCallbacks.push(callback);
    if (_sdkLoading) return;
    _sdkLoading = true;

    var script = document.createElement('script');
    script.src = TRTC_SDK_URL;
    script.onload = function() {
      _sdkLoaded = true;
      _sdkLoading = false;
      _sdkLoadCallbacks.forEach(function(cb) { cb(null); });
      _sdkLoadCallbacks = [];
    };
    script.onerror = function() {
      _sdkLoading = false;
      var err = new Error('TRTC SDK 加载失败');
      _sdkLoadCallbacks.forEach(function(cb) { cb(err); });
      _sdkLoadCallbacks = [];
    };
    document.head.appendChild(script);
  }

  // ===== UserSig 获取 =====
  function getToken() {
    var t = localStorage.getItem('im_token') || '';
    if (t) return t;
    // 尝试从 cookie 或全局变量中获取
    if (window.__im_token) return window.__im_token;
    return '';
  }

  function getUserId() {
    // 从 localStorage 获取当前登录用户的 uid
    var uid = localStorage.getItem('im_uid') || localStorage.getItem('im_user_id') || '';
    if (uid) return uid;
    // 尝试从 DOM 获取
    var userEl = document.querySelector('[data-uid]');
    if (userEl) return userEl.getAttribute('data-uid');
    // 生成临时 ID
    return 'user_' + Date.now();
  }

  function fetchUserSig(callback) {
    var token = getToken();
    var userId = getUserId();
    if (!token) {
      callback(new Error('未登录，请先登录'));
      return;
    }

    var xhr = new XMLHttpRequest();
    xhr.open('POST', TRTC_API, true);
    xhr.setRequestHeader('Content-Type', 'application/json');
    xhr.setRequestHeader('Authorization', 'Bearer ' + token);
    xhr.timeout = 5000;
    xhr.onload = function() {
      if (xhr.status === 200) {
        try {
          var resp = JSON.parse(xhr.responseText);
          if (resp.code === 200 && resp.data) {
            callback(null, resp.data);
          } else {
            callback(new Error(resp.msg || '获取通话凭证失败'));
          }
        } catch(e) {
          callback(new Error('解析响应失败'));
        }
      } else {
        callback(new Error('获取通话凭证失败 (' + xhr.status + ')'));
      }
    };
    xhr.onerror = function() { callback(new Error('网络错误')); };
    xhr.ontimeout = function() { callback(new Error('请求超时')); };
    xhr.send(JSON.stringify({ userId: userId }));
  }

  // ===== 通话界面 =====
  function createCallUI() {
    if (document.getElementById('imim-call-overlay')) return;

    var overlay = document.createElement('div');
    overlay.id = 'imim-call-overlay';
    overlay.style.cssText = [
      'position:fixed;top:0;left:0;width:100%;height:100%;z-index:99999;',
      'background:#1a1a2e;display:none;flex-direction:column;',
      'font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;'
    ].join('');

    overlay.innerHTML = [
      '<div id="imim-call-remote" style="flex:1;display:flex;align-items:center;justify-content:center;position:relative;">',
        '<div id="imim-call-remote-video" style="width:100%;height:100%;display:flex;align-items:center;justify-content:center;"></div>',
        '<div id="imim-call-status" style="position:absolute;top:50%;left:50%;transform:translate(-50%,-50%);color:#fff;font-size:16px;text-align:center;">',
          '<div style="font-size:48px;margin-bottom:16px;">📞</div>',
          '<div id="imim-call-status-text">正在连接...</div>',
        '</div>',
      '</div>',
      '<div id="imim-call-local" style="position:absolute;top:16px;right:16px;width:120px;height:160px;border-radius:12px;overflow:hidden;background:#333;z-index:10;border:2px solid rgba(255,255,255,0.3);">',
        '<div id="imim-call-local-video" style="width:100%;height:100%;"></div>',
      '</div>',
      '<div id="imim-call-controls" style="display:flex;justify-content:center;align-items:center;gap:24px;padding:24px 16px 40px;background:rgba(0,0,0,0.5);">',
        '<button id="imim-call-mute-btn" style="width:56px;height:56px;border-radius:50%;border:none;background:rgba(255,255,255,0.2);cursor:pointer;font-size:24px;display:flex;align-items:center;justify-content:center;transition:background 0.2s;" title="静音">',
          '<span id="imim-call-mute-icon">🎤</span>',
        '</button>',
        '<button id="imim-call-video-btn" style="width:56px;height:56px;border-radius:50%;border:none;background:rgba(255,255,255,0.2);cursor:pointer;font-size:24px;display:flex;align-items:center;justify-content:center;transition:background 0.2s;" title="摄像头">',
          '<span id="imim-call-video-icon">📹</span>',
        '</button>',
        '<button id="imim-call-hangup-btn" style="width:64px;height:64px;border-radius:50%;border:none;background:#e74c3c;cursor:pointer;font-size:28px;display:flex;align-items:center;justify-content:center;transition:transform 0.2s;" title="挂断">',
          '<span>📞</span>',
        '</button>',
        '<button id="imim-call-speaker-btn" style="width:56px;height:56px;border-radius:50%;border:none;background:rgba(255,255,255,0.2);cursor:pointer;font-size:24px;display:flex;align-items:center;justify-content:center;transition:background 0.2s;" title="扬声器">',
          '<span id="imim-call-speaker-icon">🔊</span>',
        '</button>',
      '</div>',
    ].join('');

    document.body.appendChild(overlay);

    // 按钮事件
    var isMuted = false;
    var isVideoOff = false;

    document.getElementById('imim-call-mute-btn').onclick = function() {
      isMuted = !isMuted;
      if (_trtcClient) {
        if (isMuted) {
          _trtcClient.stopLocalAudio();
        } else {
          _trtcClient.startLocalAudio();
        }
      }
      document.getElementById('imim-call-mute-icon').textContent = isMuted ? '🔇' : '🎤';
      this.style.background = isMuted ? 'rgba(231,76,60,0.6)' : 'rgba(255,255,255,0.2)';
    };

    document.getElementById('imim-call-video-btn').onclick = function() {
      isVideoOff = !isVideoOff;
      if (_trtcClient) {
        if (isVideoOff) {
          _trtcClient.stopLocalVideo();
        } else {
          _trtcClient.startLocalVideo({ view: 'imim-call-local-video' });
        }
      }
      document.getElementById('imim-call-video-icon').textContent = isVideoOff ? '📷' : '📹';
      this.style.background = isVideoOff ? 'rgba(231,76,60,0.6)' : 'rgba(255,255,255,0.2)';
      document.getElementById('imim-call-local').style.display = isVideoOff ? 'none' : 'block';
    };

    document.getElementById('imim-call-hangup-btn').onclick = function() {
      endCall();
    };
  }

  // ===== 通话核心逻辑 =====
  function startCall(remoteUserId) {
    if (_inCall) {
      console.warn('[TRTC] 已在通话中');
      return;
    }

    createCallUI();
    showCallStatus('正在连接...');

    loadTRTCSDK(function(err) {
      if (err) {
        showCallStatus('SDK 加载失败: ' + err.message);
        return;
      }

      fetchUserSig(function(err, data) {
        if (err) {
          showCallStatus('鉴权失败: ' + err.message);
          return;
        }

        initTRTCClient(data, remoteUserId);
      });
    });
  }

  function initTRTCClient(userSigData, remoteUserId) {
    try {
      _trtcClient = TRTC.create();

      _callRoomId = Math.floor(Math.random() * 100000) + 10000;
      _callUserId = userSigData.userId;
      _inCall = true;

      // 显示通话界面
      document.getElementById('imim-call-overlay').style.display = 'flex';

      // 进入房间
      _trtcClient.enterRoom({
        sdkAppId: userSigData.sdkAppId,
        userId: userSigData.userId,
        userSig: userSigData.userSig,
        roomId: _callRoomId,
      }).then(function() {
        showCallStatus('');
        // 开启本地音视频
        return _trtcClient.startLocalVideo({ view: 'imim-call-local-video' });
      }).then(function() {
        return _trtcClient.startLocalAudio();
      }).catch(function(err) {
        console.error('[TRTC] 进房失败:', err);
        showCallStatus('连接失败');
        setTimeout(endCall, 2000);
      });

      // 监听远端视频
      _trtcClient.on(TRTC.EVENT.REMOTE_VIDEO_AVAILABLE, function(event) {
        if (event.userId) {
          _trtcClient.startRemoteVideo({
            userId: event.userId,
            streamType: TRTC.TYPE.STREAM_TYPE_MAIN,
            view: 'imim-call-remote-video',
          });
          showCallStatus('');
        }
      });

      // 监听远端音频
      _trtcClient.on(TRTC.EVENT.REMOTE_AUDIO_AVAILABLE, function(event) {
        showCallStatus('');
      });

      // 监听远端用户进入
      _trtcClient.on(TRTC.EVENT.REMOTE_USER_ENTER, function(event) {
        showCallStatus('对方已加入');
        setTimeout(function() { showCallStatus(''); }, 2000);
      });

      // 监听远端用户退出
      _trtcClient.on(TRTC.EVENT.REMOTE_USER_EXIT, function(event) {
        showCallStatus('对方已挂断');
        setTimeout(endCall, 2000);
      });

      // 监听错误
      _trtcClient.on(TRTC.EVENT.ERROR, function(err) {
        console.error('[TRTC] 错误:', err);
        showCallStatus('通话异常');
      });

      // 网络质量
      _trtcClient.on(TRTC.EVENT.NETWORK_QUALITY, function(event) {
        // 可在此显示网络状态
      });

    } catch(e) {
      console.error('[TRTC] 初始化失败:', e);
      showCallStatus('初始化失败');
      _inCall = false;
    }
  }

  function endCall() {
    _inCall = false;
    showCallStatus('通话已结束');

    if (_trtcClient) {
      try {
        _trtcClient.stopLocalAudio();
        _trtcClient.stopLocalVideo();
        _trtcClient.exitRoom();
        _trtcClient.destroy();
      } catch(e) {}
      _trtcClient = null;
    }

    setTimeout(function() {
      var overlay = document.getElementById('imim-call-overlay');
      if (overlay) overlay.style.display = 'none';
      // 清理本地视频容器
      var localVideo = document.getElementById('imim-call-local-video');
      if (localVideo) localVideo.innerHTML = '';
      var remoteVideo = document.getElementById('imim-call-remote-video');
      if (remoteVideo) remoteVideo.innerHTML = '';
    }, 500);
  }

  function showCallStatus(text) {
    var el = document.getElementById('imim-call-status');
    var textEl = document.getElementById('imim-call-status-text');
    if (textEl) textEl.textContent = text;
    if (el) el.style.display = text ? 'block' : 'none';
  }

  // ===== 聊天页注入视频通话按钮 =====
  function injectCallButton() {
    // 在聊天窗口顶部注入通话按钮
    var attempts = 0;
    var maxAttempts = 10;

    function tryInject() {
      attempts++;
      // 查找聊天页顶部区域
      var chatHeader = document.querySelector('.wk-chat-header, [class*="chat-header"], [class*="ChatHeader"]');
      if (!chatHeader) {
        chatHeader = document.querySelector('.wk-chat-open .wk-layout-content-right-header, [class*="conversation-header"]');
      }
      if (!chatHeader) {
        chatHeader = document.querySelector('.wk-layout-content-right [class*="header"]');
      }

      if (chatHeader && !document.getElementById('imim-call-btn')) {
        var btn = document.createElement('button');
        btn.id = 'imim-call-btn';
        btn.title = '视频通话';
        btn.style.cssText = [
          'width:36px;height:36px;border-radius:50%;border:none;',
          'background:rgba(22,119,255,0.1);cursor:pointer;',
          'font-size:18px;display:flex;align-items:center;justify-content:center;',
          'margin-left:8px;transition:background 0.2s;'
        ].join('');
        btn.innerHTML = '📹';
        btn.onmouseenter = function() { this.style.background = 'rgba(22,119,255,0.2)'; };
        btn.onmouseleave = function() { this.style.background = 'rgba(22,119,255,0.1)'; };
        btn.onclick = function(e) {
          e.stopPropagation();
          // 获取对方用户 ID
          var remoteId = getRemoteUserId();
          if (remoteId) {
            startCall(remoteId);
          } else {
            alert('无法获取对方用户信息');
          }
        };
        chatHeader.appendChild(btn);
        return true;
      }
      return false;
    }

    if (!tryInject() && attempts < maxAttempts) {
      setTimeout(tryInject, 1000);
    }
  }

  function getRemoteUserId() {
    // 尝试从聊天页面获取对方 UID
    var chatTitle = document.querySelector('.wk-chat-header-title, [class*="chat-title"], [class*="conversation-title"]');
    if (chatTitle) {
      var text = chatTitle.textContent || '';
      // 尝试提取手机号
      var match = text.match(/1[3-9]\d{9}/);
      if (match) return match[0];
      return text.trim();
    }
    return '';
  }

  // ===== 拦截并保存登录 token =====
  function setupTokenCapture() {
    var origFetch = window.fetch;
    window.fetch = function(url, options) {
      var p = origFetch.apply(this, arguments);
      if (url && url.toString().indexOf('user/login') !== -1) {
        p.then(function(resp) {
          var clone = resp.clone();
          clone.json().then(function(data) {
            if (data.uid) {
              localStorage.setItem('im_uid', data.uid);
              localStorage.setItem('im_user_id', data.uid);
            }
            if (data.token) {
              localStorage.setItem('im_token', data.token);
              window.__im_token = data.token;
            }
          }).catch(function() {});
        }).catch(function() {});
      }
      return p;
    };

    // 同时也拦截 XHR
    var origXHRSend = XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.send = function(body) {
      var xhr = this;
      if (xhr._xhrUrl && xhr._xhrUrl.toString().indexOf('user/login') !== -1) {
        var origOnLoad = xhr.onload;
        xhr.addEventListener('load', function() {
          try {
            var data = JSON.parse(xhr.responseText);
            if (data.uid) {
              localStorage.setItem('im_uid', data.uid);
              localStorage.setItem('im_user_id', data.uid);
            }
            if (data.token) {
              localStorage.setItem('im_token', data.token);
              window.__im_token = data.token;
            }
          } catch(e) {}
        });
      }
      return origXHRSend.apply(this, arguments);
    };
  }

  // ===== 初始化 =====
  function init() {
    setupTokenCapture();
    createCallUI();

    // 监听聊天页面打开，注入通话按钮
    var observer = new MutationObserver(function() {
      if (document.querySelector('.wk-chat-open, [class*="chat-open"]')) {
        injectCallButton();
      }
    });
    observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'] });

    // 定时尝试注入（处理动态加载场景）
    setTimeout(injectCallButton, 3000);
    setTimeout(injectCallButton, 6000);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
