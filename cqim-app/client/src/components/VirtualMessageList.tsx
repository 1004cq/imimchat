/**
 * CQIM 统一窗口化消息列表
 *
 * 只渲染视口附近的消息，消息高度通过 ResizeObserver 动态测量；
 * 不依赖第三方虚拟列表库，群聊与私聊都可以复用。
 * 按 chatId + messageId 持久化/恢复阅读位置（避免动态高度下 scrollTop 错位）。
 */
import React, {
  forwardRef,
  memo,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { springBadge } from '@/lib/animations';
import {
  clearChatScrollAnchor,
  loadChatScrollAnchor,
  saveChatScrollAnchor,
} from '@/lib/chatScrollAnchor';

export interface VirtualMessageItem {
  id?: string;
  seq?: number;
  senderId: string;
  senderName?: string;
  msgType?: string;
  content: string;
  timestamp: number;
  status?: 'sending' | 'sent' | 'delivered' | 'failed' | string;
  isRevoked?: boolean;
}

export interface VirtualMessageListHandle {
  scrollToBottom: (behavior?: ScrollBehavior) => void;
  getVisibleAnchorMessageId: () => string | null;
  persistAnchor: () => void;
}

interface VirtualMessageListProps {
  messages: VirtualMessageItem[];
  currentUserId: string;
  loading?: boolean;
  hasMore?: boolean;
  onLoadMore?: () => void;
  loadMoreAt?: 'top' | 'bottom';
  renderMessage?: (
    msg: VirtualMessageItem,
    isOwn: boolean,
    index: number,
    previous?: VirtualMessageItem,
  ) => React.ReactNode;
  className?: string;
  height?: number | string;
  estimatedRowHeight?: number;
  /** 会话 ID：用于按会话隔离阅读锚点 */
  chatId?: string | null;
}

const DEFAULT_ROW_HEIGHT = 76;
const OVERSCAN_PX = 900;
const LOAD_MORE_THRESHOLD = 240;

function formatTime(ts: number): string {
  const d = new Date(ts);
  const now = new Date();
  const hours = d.getHours().toString().padStart(2, '0');
  const mins = d.getMinutes().toString().padStart(2, '0');
  if (d.toDateString() === now.toDateString()) return `${hours}:${mins}`;
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return `昨天 ${hours}:${mins}`;
  return `${d.getMonth() + 1}/${d.getDate()} ${hours}:${mins}`;
}

function shouldShowTimeSeparator(current: VirtualMessageItem, previous?: VirtualMessageItem): boolean {
  return !previous || current.timestamp - previous.timestamp > 5 * 60 * 1000;
}

const DefaultMessageBubble = memo(({ msg, isOwn }: { msg: VirtualMessageItem; isOwn: boolean }) => {
  if (msg.msgType === 'system') {
    return (
      <div className="flex justify-center py-1.5">
        <span className="text-xs text-dove-ink/40 bg-dove-mist/50 px-3 py-1 rounded-full">{msg.content}</span>
      </div>
    );
  }

  return (
    <div className={`flex ${isOwn ? 'justify-end' : 'justify-start'} px-3 py-0.5 message-item`}>
      {!isOwn && (
        <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-dove-green/20 to-dove-bamboo/20 flex items-center justify-center text-xs font-medium text-dove-green mr-2 mt-0.5 flex-shrink-0">
          {((msg.senderName || msg.senderId) || '?').charAt(0).toUpperCase()}
        </div>
      )}
      <div className={`max-w-[70%] ${isOwn ? 'items-end' : 'items-start'} flex flex-col`}>
        {!isOwn && <span className="text-[10px] text-dove-ink/40 mb-0.5 ml-1">{msg.senderName || msg.senderId}</span>}
        <div className={`relative px-3 py-2 rounded-2xl text-sm leading-relaxed break-words gpu-accelerated ${isOwn ? 'bg-gradient-to-br from-dove-green to-dove-bamboo text-white rounded-tr-md' : 'bg-white dark:bg-dove-ink/10 text-dove-ink rounded-tl-md shadow-soft-sm'}`}>
          {msg.content}
          {isOwn && msg.status && (
            <span className="inline-block ml-1.5 align-bottom">
              {msg.status === 'sending' && <span className="inline-block w-3 h-3 border border-white/50 border-t-transparent rounded-full animate-spin" />}
              {(msg.status === 'sent' || msg.status === 'delivered') && (
                <svg className="inline w-3 h-3 text-white/70" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                </svg>
              )}
            </span>
          )}
        </div>
        <span className={`text-[10px] text-dove-ink/30 mt-0.5 ${isOwn ? 'mr-1' : 'ml-1'}`}>{formatTime(msg.timestamp)}</span>
      </div>
    </div>
  );
});
DefaultMessageBubble.displayName = 'DefaultMessageBubble';

const TimeSeparator = memo(({ timestamp }: { timestamp: number }) => (
  <div className="flex justify-center py-2">
    <span className="text-[10px] text-dove-ink/30 bg-dove-mist/30 px-2.5 py-0.5 rounded-full">{formatTime(timestamp)}</span>
  </div>
));
TimeSeparator.displayName = 'TimeSeparator';

const MessageSkeleton = memo(() => (
  <div className="px-3 py-3 space-y-3">
    {[...Array(5)].map((_, i) => (
      <div key={i} className={`flex gap-2 ${i % 2 === 0 ? '' : 'flex-row-reverse'}`}>
        <div className="w-8 h-8 rounded-lg skeleton-enhanced flex-shrink-0" />
        <div className="flex flex-col gap-1.5 flex-1" style={{ maxWidth: '60%' }}>
          <div className={`h-3 skeleton-enhanced rounded ${i % 3 === 0 ? 'w-3/4' : 'w-1/2'}`} />
          <div className="h-8 skeleton-enhanced rounded-2xl" />
        </div>
      </div>
    ))}
  </div>
));
MessageSkeleton.displayName = 'MessageSkeleton';

export const VirtualMessageList = memo(forwardRef<VirtualMessageListHandle, VirtualMessageListProps>(function VirtualMessageList({
  messages,
  currentUserId,
  loading = false,
  hasMore = true,
  onLoadMore,
  loadMoreAt = 'top',
  renderMessage,
  className = '',
  height,
  estimatedRowHeight = DEFAULT_ROW_HEIGHT,
  chatId = null,
}, ref) {
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const rowHeightsRef = useRef(new Map<string, number>());
  const rowObserversRef = useRef(new Map<string, ResizeObserver>());
  const prependScrollRef = useRef<{ height: number; top: number } | null>(null);
  const isLoadingMoreRef = useRef(false);
  const isAtBottomRef = useRef(true);
  const prevLengthRef = useRef(0);
  const rafRef = useRef<number | null>(null);
  const saveAnchorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingAnchorRef = useRef<string | null | undefined>(undefined);
  const restoreAttemptsRef = useRef(0);
  const activeChatIdRef = useRef<string | null>(chatId || null);
  const messagesRef = useRef(messages);
  const safeMessages = useMemo(
    () => (Array.isArray(messages) ? messages : []).filter(msg => !!msg && !!msg.senderId && !!(msg.id || msg.seq)),
    [messages],
  );
  messagesRef.current = safeMessages;

  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(0);
  const [layoutVersion, setLayoutVersion] = useState(0);
  const [showNewMsgTip, setShowNewMsgTip] = useState(false);
  const [newMsgCount, setNewMsgCount] = useState(0);

  const getKey = useCallback((msg: VirtualMessageItem, index: number) => `${msg.id || msg.seq || 'message'}-${index}`, []);
  const getHeight = useCallback((msg: VirtualMessageItem, index: number) => rowHeightsRef.current.get(getKey(msg, index)) || estimatedRowHeight, [estimatedRowHeight, getKey]);

  const getRange = useCallback(() => {
    const startLimit = Math.max(0, scrollTop - OVERSCAN_PX);
    const endLimit = scrollTop + viewportHeight + OVERSCAN_PX;
    let offset = 0;
    let start = 0;
    let end = safeMessages.length;
    for (let i = 0; i < safeMessages.length; i += 1) {
      const next = offset + getHeight(safeMessages[i], i);
      if (next >= startLimit) { start = i; break; }
      offset = next;
    }
    offset = 0;
    for (let i = 0; i < safeMessages.length; i += 1) {
      offset += getHeight(safeMessages[i], i);
      if (offset >= endLimit) { end = i + 1; break; }
    }
    return { start, end };
  }, [getHeight, safeMessages, scrollTop, viewportHeight]);

  const getOffsetBefore = useCallback((index: number) => {
    let offset = 0;
    for (let i = 0; i < index; i += 1) offset += getHeight(safeMessages[i], i);
    return offset;
  }, [getHeight, safeMessages]);

  const totalHeight = safeMessages.reduce((sum, msg, index) => sum + getHeight(msg, index), 0);
  const { start, end } = getRange();

  const getVisibleAnchorMessageId = useCallback((): string | null => {
    const list = messagesRef.current;
    if (!list.length) return null;
    const container = scrollContainerRef.current;
    if (!container) return list[list.length - 1]?.id || null;
    const top = container.scrollTop + 12;
    let offset = 0;
    for (let i = 0; i < list.length; i += 1) {
      const h = rowHeightsRef.current.get(`${list[i].id || list[i].seq || 'message'}-${i}`) || estimatedRowHeight;
      if (offset + h > top) return list[i].id || null;
      offset += h;
    }
    return list[list.length - 1]?.id || null;
  }, [estimatedRowHeight]);

  const persistAnchor = useCallback(() => {
    const id = activeChatIdRef.current;
    if (!id) return;
    const container = scrollContainerRef.current;
    if (container) {
      const nearBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 100;
      if (nearBottom) {
        clearChatScrollAnchor(id);
        return;
      }
    }
    const msgId = getVisibleAnchorMessageId();
    if (msgId) saveChatScrollAnchor(id, msgId);
  }, [getVisibleAnchorMessageId]);

  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const container = scrollContainerRef.current;
    if (container) {
      if (behavior === 'auto') {
        container.scrollTop = container.scrollHeight;
        setScrollTop(container.scrollTop);
      } else {
        bottomRef.current?.scrollIntoView({ behavior });
      }
    } else {
      bottomRef.current?.scrollIntoView({ behavior });
    }
    isAtBottomRef.current = true;
    setShowNewMsgTip(false);
    setNewMsgCount(0);
    const id = activeChatIdRef.current;
    if (id) clearChatScrollAnchor(id);
  }, []);

  const scrollToMessageId = useCallback((messageId: string) => {
    const container = scrollContainerRef.current;
    const list = messagesRef.current;
    if (!container || !list.length) return false;
    const index = list.findIndex(m => m.id === messageId);
    if (index < 0) return false;
    let offset = 0;
    for (let i = 0; i < index; i += 1) {
      offset += rowHeightsRef.current.get(`${list[i].id || list[i].seq || 'message'}-${i}`) || estimatedRowHeight;
    }
    container.scrollTop = offset;
    setScrollTop(offset);
    isAtBottomRef.current = container.scrollHeight - container.scrollTop - container.clientHeight < 100;
    return true;
  }, [estimatedRowHeight]);

  useImperativeHandle(ref, () => ({
    scrollToBottom,
    getVisibleAnchorMessageId,
    persistAnchor,
  }), [scrollToBottom, getVisibleAnchorMessageId, persistAnchor]);

  // 切换会话：重置恢复状态，并加载该会话锚点
  useLayoutEffect(() => {
    activeChatIdRef.current = chatId || null;
    restoreAttemptsRef.current = 0;
    if (chatId) {
      pendingAnchorRef.current = loadChatScrollAnchor(chatId);
    } else {
      pendingAnchorRef.current = undefined;
    }
    rowHeightsRef.current.clear();
    isAtBottomRef.current = true;
    prevLengthRef.current = 0;
  }, [chatId]);

  // 消息就绪后恢复锚点；无锚点或消息不在列表则滚到底
  useLayoutEffect(() => {
    if (!chatId || safeMessages.length === 0) return;
    if (pendingAnchorRef.current === undefined) return;

    const container = scrollContainerRef.current;
    if (!container) return;

    const anchor = pendingAnchorRef.current;
    if (!anchor) {
      container.scrollTop = container.scrollHeight;
      setScrollTop(container.scrollTop);
      isAtBottomRef.current = true;
      pendingAnchorRef.current = undefined;
      return;
    }

    const ok = scrollToMessageId(anchor);
    restoreAttemptsRef.current += 1;
    // 高度测量稳定后再结束，或消息不在列表 / 尝试过多则到底部
    if (!ok) {
      container.scrollTop = container.scrollHeight;
      setScrollTop(container.scrollTop);
      isAtBottomRef.current = true;
      pendingAnchorRef.current = undefined;
      clearChatScrollAnchor(chatId);
      return;
    }
    if (restoreAttemptsRef.current >= 4 || layoutVersion >= 3) {
      pendingAnchorRef.current = undefined;
    }
  }, [chatId, safeMessages.length, layoutVersion, scrollToMessageId]);

  const handleScroll = useCallback(() => {
    const container = scrollContainerRef.current;
    if (!container) return;
    isAtBottomRef.current = container.scrollHeight - container.scrollTop - container.clientHeight < 100;
    if (isAtBottomRef.current) {
      setShowNewMsgTip(false);
      setNewMsgCount(0);
    }
    if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      setScrollTop(container.scrollTop);
      setViewportHeight(container.clientHeight);
    });
    const nearLoadBoundary = loadMoreAt === 'bottom'
      ? container.scrollHeight - container.scrollTop - container.clientHeight < LOAD_MORE_THRESHOLD
      : container.scrollTop < LOAD_MORE_THRESHOLD;
    if (nearLoadBoundary && hasMore && onLoadMore && !isLoadingMoreRef.current) {
      isLoadingMoreRef.current = true;
      prependScrollRef.current = loadMoreAt === 'top'
        ? { height: container.scrollHeight, top: container.scrollTop }
        : null;
      onLoadMore();
      window.setTimeout(() => { isLoadingMoreRef.current = false; }, 600);
    }
    // 恢复完成后才持久化锚点，避免覆盖
    if (pendingAnchorRef.current === undefined && activeChatIdRef.current) {
      if (saveAnchorTimerRef.current) clearTimeout(saveAnchorTimerRef.current);
      saveAnchorTimerRef.current = setTimeout(() => {
        persistAnchor();
      }, 200);
    }
  }, [hasMore, loadMoreAt, onLoadMore, persistAnchor]);

  useLayoutEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;
    const observer = new ResizeObserver(() => {
      setViewportHeight(container.clientHeight);
      setLayoutVersion(version => version + 1);
    });
    observer.observe(container);
    setViewportHeight(container.clientHeight);
    setScrollTop(container.scrollTop);
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    const pending = prependScrollRef.current;
    const container = scrollContainerRef.current;
    if (!pending || !container) return;
    const delta = container.scrollHeight - pending.height;
    if (delta > 0) container.scrollTop = pending.top + delta;
    prependScrollRef.current = null;
    setScrollTop(container.scrollTop);
  }, [safeMessages.length, layoutVersion]);

  useEffect(() => {
    const added = safeMessages.length - prevLengthRef.current;
    if (added > 0 && pendingAnchorRef.current === undefined) {
      if (isAtBottomRef.current) {
        requestAnimationFrame(() => bottomRef.current?.scrollIntoView({ behavior: added <= 3 ? 'smooth' : 'auto' }));
      } else {
        setNewMsgCount(count => count + added);
        setShowNewMsgTip(true);
      }
    }
    prevLengthRef.current = safeMessages.length;
  }, [safeMessages.length]);

  useEffect(() => () => {
    if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
    if (saveAnchorTimerRef.current) clearTimeout(saveAnchorTimerRef.current);
    persistAnchor();
    rowObserversRef.current.forEach(observer => observer.disconnect());
    rowObserversRef.current.clear();
  }, [persistAnchor]);

  const measureRow = useCallback((key: string, node: HTMLDivElement | null) => {
    const previousObserver = rowObserversRef.current.get(key);
    previousObserver?.disconnect();
    rowObserversRef.current.delete(key);
    if (!node || typeof ResizeObserver === 'undefined') return;

    const observer = new ResizeObserver(entries => {
      const measured = entries[0]?.contentRect.height;
      if (measured && Math.abs((rowHeightsRef.current.get(key) || 0) - measured) > 1) {
        rowHeightsRef.current.set(key, measured);
        setLayoutVersion(version => version + 1);
      }
    });
    rowObserversRef.current.set(key, observer);
    observer.observe(node);
  }, []);

  return (
    <div className={`relative flex-1 overflow-hidden ${className}`} style={height !== undefined ? { height } : undefined}>
      <div
        ref={scrollContainerRef}
        onScroll={handleScroll}
        className="h-full overflow-y-auto overscroll-contain chat-messages-container"
        style={{ WebkitOverflowScrolling: 'touch' }}
      >
        {loading && hasMore && <div className="flex justify-center py-3"><span className="w-4 h-4 border-2 border-dove-green/30 border-t-dove-green rounded-full animate-spin" /></div>}
        {loading && safeMessages.length === 0 && <MessageSkeleton />}
        {!hasMore && safeMessages.length > 0 && <div className="flex justify-center py-3"><span className="text-[10px] text-dove-ink/25">—— 已无更多消息 ——</span></div>}
        <div style={{ height: totalHeight, position: 'relative' }}>
          {safeMessages.slice(start, end).map((msg, visibleIndex) => {
            const index = start + visibleIndex;
            const key = getKey(msg, index);
            const previous = index > 0 ? safeMessages[index - 1] : undefined;
            const isOwn = msg.senderId === currentUserId;
            return (
              <div
                key={key}
                ref={node => measureRow(key, node)}
                className="absolute left-0 right-0"
                style={{ transform: `translateY(${getOffsetBefore(index)}px)`, contain: 'layout paint style' }}
              >
                {shouldShowTimeSeparator(msg, previous) && <TimeSeparator timestamp={msg.timestamp} />}
                {renderMessage ? renderMessage(msg, isOwn, index, previous) : <DefaultMessageBubble msg={msg} isOwn={isOwn} />}
              </div>
            );
          })}
        </div>
        <div ref={bottomRef} className="h-px" />
      </div>
      <AnimatePresence>
        {showNewMsgTip && (
          <motion.button
            initial={{ opacity: 0, y: 20, scale: 0.8 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 20, scale: 0.8 }}
            transition={springBadge}
            onClick={() => scrollToBottom('smooth')}
            className="absolute bottom-4 right-4 flex items-center gap-1.5 px-3 py-1.5 bg-dove-green text-white text-xs font-medium rounded-full shadow-lg z-10"
          >
            ↓ {newMsgCount} 条新消息
          </motion.button>
        )}
      </AnimatePresence>
    </div>
  );
}));

VirtualMessageList.displayName = 'VirtualMessageList';
export default VirtualMessageList;
