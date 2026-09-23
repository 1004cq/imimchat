import React from 'react';
import { Loader2, RefreshCw, ShieldAlert } from 'lucide-react';

export interface KeySyncBannerProps {
  kind: 'private' | 'group';
  syncing?: boolean;
  error?: string | null;
  onRetry: () => void;
}

export function KeySyncBanner({ kind, syncing, error, onRetry }: KeySyncBannerProps) {
  const title = kind === 'group' ? '群安全会话未就绪' : '安全会话需要重新验证';
  const detail = error
    || (kind === 'group'
      ? '群消息仍保存在服务器，但当前设备没有可用的本地 MLS 密钥。可重试同步 Welcome/Commit；若仍失败，需要群成员重新邀请。'
      : '历史密文无法在新设备解开。重试将重建会话，之后的新消息可以正常加解密。');

  return (
    <div className="px-3 py-2 border-b border-amber-200/70 bg-amber-50 text-amber-900 dark:bg-amber-950/40 dark:text-amber-100 dark:border-amber-800/60">
      <div className="flex items-start gap-2">
        {syncing ? (
          <Loader2 size={14} className="mt-0.5 flex-shrink-0 animate-spin" />
        ) : (
          <ShieldAlert size={14} className="mt-0.5 flex-shrink-0" />
        )}
        <div className="min-w-0 flex-1">
          <p className="text-[12px] font-medium">{syncing ? '正在同步密钥…' : title}</p>
          <p className="text-[11px] opacity-80 leading-snug mt-0.5">{detail}</p>
        </div>
        <button
          type="button"
          onClick={onRetry}
          disabled={syncing}
          className="inline-flex items-center gap-1 flex-shrink-0 rounded-full border border-amber-300/80 px-2 py-1 text-[11px] font-medium disabled:opacity-50"
        >
          <RefreshCw size={11} className={syncing ? 'animate-spin' : ''} />
          重试
        </button>
      </div>
    </div>
  );
}

export default KeySyncBanner;
