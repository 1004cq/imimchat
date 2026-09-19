import React, { useEffect, useRef } from 'react';
import { TUICallKit, TUICallKitAPI } from '@trtc/calls-uikit-react';

let globalInitialized = false;
let globalInitializing = false;

export async function initTUICallKit(token: string, userId: string, nickname: string, avatar: string): Promise<boolean> {
  if (globalInitialized) return true;
  if (globalInitializing) return false;
  globalInitializing = true;

  try {
    const res = await fetch('/api/trtc/usersig', {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      globalInitializing = false;
      return false;
    }
    const data = await res.json();
    const { userSig, sdkAppId } = data;
    if (!userSig || !sdkAppId) {
      console.error('[TUICallKit] usersig 缺 sdkAppId，禁止回落 旧 TRTC AppID');
      globalInitializing = false;
      return false;
    }

    await TUICallKitAPI.init({
      userID: String(userId),
      userSig,
      SDKAppID: Number(sdkAppId),
    });
    try {
      await TUICallKitAPI.setSelfInfo({
        nickName: nickname || String(userId),
        avatar: avatar || '',
      });
    } catch {
      /* ignore */
    }
    globalInitialized = true;
    globalInitializing = false;
    return true;
  } catch (err) {
    console.error('[TUICallKit] 初始化失败:', err);
    globalInitializing = false;
    return false;
  }
}

export function resetTUICallKit() {
  globalInitialized = false;
  globalInitializing = false;
}

export function isTUICallKitInitialized() {
  return globalInitialized;
}

export const TUICallKitWrapper: React.FC = () => {
  const initAttempted = useRef(false);
  useEffect(() => {
    if (initAttempted.current) return;
    initAttempted.current = true;
    const token = localStorage.getItem('user_token');
    const userId = localStorage.getItem('user_id');
    const nickname = localStorage.getItem('user_nickname') || localStorage.getItem('user_username') || '';
    const avatar = localStorage.getItem('user_avatar') || '';
    if (token && userId) initTUICallKit(token, userId, nickname, avatar);
  }, []);
  return (
    <div id="tuicallkit-root" style={{ position: 'fixed', width: 0, height: 0, zIndex: 9999, pointerEvents: 'none' }}>
      <div style={{ pointerEvents: 'auto' }}>
        <TUICallKit />
      </div>
    </div>
  );
};

export default TUICallKitWrapper;
