/**
 * 全局搜索页 — 架构图「搜索服务：消息 / 群 / 频道 / 文件」
 */
import React, { useState, useEffect, useCallback } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { ArrowLeft, Search, MessageSquare, Users, Hash, FileText, Loader2, X } from 'lucide-react';
import { authApi } from '@/lib/authFetch';
import { DoveAvatar } from '@/components/DoveAvatar';
import { useApp, useAppActions } from '@/contexts/AppContext';

type SearchScope = 'all' | 'messages' | 'users' | 'groups' | 'channels' | 'files';

interface SearchResult {
  messages: Array<{
    id: string;
    type: 'private' | 'group';
    conversationId: string;
    conversationName: string;
    senderName: string;
    content: string;
    highlight?: string;
    timestamp: string;
  }>;
  users: Array<{ id: string; username: string; nickname: string; avatar: string; isBot: boolean }>;
  groups: Array<{ id: string; name: string; username: string | null; memberCount: number; avatar: string | null }>;
  channels: Array<{ id: string; name: string; username: string | null; memberCount: number; avatar: string | null }>;
  files: Array<{ id: string; filename: string; type: string; url: string }>;
  total: number;
}

const TABS: { key: SearchScope; label: string; icon: React.ElementType }[] = [
  { key: 'all', label: '全部', icon: Search },
  { key: 'messages', label: '消息', icon: MessageSquare },
  { key: 'users', label: '联系人', icon: Users },
  { key: 'groups', label: '群组', icon: Users },
  { key: 'channels', label: '频道', icon: Hash },
  { key: 'files', label: '文件', icon: FileText },
];

interface Props {
  onClose: () => void;
}

export default function GlobalSearchPage({ onClose }: Props) {
  const { state } = useApp();
  const { openChat } = useAppActions();
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState<SearchScope>('all');
  const [results, setResults] = useState<SearchResult | null>(null);
  const [loading, setLoading] = useState(false);

  const userId = state.currentUser?.id;

  const doSearch = useCallback(async (q: string, s: SearchScope) => {
    if (!q.trim()) {
      setResults(null);
      return;
    }
    setLoading(true);
    try {
      const data = await authApi(
        `/api/search?q=${encodeURIComponent(q)}&scope=${s}&limit=30`
      ) as SearchResult;
      setResults(data);
    } catch {
      setResults({ messages: [], users: [], groups: [], channels: [], files: [], total: 0 });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const timer = setTimeout(() => doSearch(query, scope), 300);
    return () => clearTimeout(timer);
  }, [query, scope, doSearch]);

  const handleOpenMessage = (convId: string, type: 'private' | 'group') => {
    const chat = state.chats.find((c: { id: string; groupId?: string; members?: string[] }) =>
      type === 'group' ? c.groupId === convId : c.id === convId || c.members?.includes(convId)
    );
    if (chat) {
      openChat(chat.id);
      onClose();
    }
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 20 }}
      className="fixed inset-0 z-[100] bg-background flex flex-col"
    >
      {/* 顶栏 */}
      <div className="dove-topbar gap-3">
        <button onClick={onClose} className="dove-icon-btn">
          <ArrowLeft size={20} />
        </button>
        <div className="flex-1 search-bar focused">
          <Search size={15} className="text-dove-green flex-shrink-0" />
          <input
            type="text"
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="搜索消息、联系人、群组、频道..."
            autoFocus
          />
          {query && (
            <button onClick={() => setQuery('')}>
              <X size={14} className="text-muted-foreground" />
            </button>
          )}
        </div>
      </div>

      {/* Scope Tabs */}
      <div className="flex gap-1 px-4 py-2 overflow-x-auto scrollbar-hide">
        {TABS.map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            onClick={() => setScope(key)}
            className={`flex items-center gap-1 px-3 py-1.5 rounded-full text-xs font-medium whitespace-nowrap transition-all ${
              scope === key
                ? 'bg-dove-green text-white'
                : 'bg-dove-warm-gray/60 text-muted-foreground'
            }`}
          >
            <Icon size={11} />
            {label}
          </button>
        ))}
      </div>

      {/* 结果 */}
      <div className="flex-1 overflow-y-auto px-4 pb-6">
        {loading && (
          <div className="flex items-center justify-center py-12 gap-2 text-muted-foreground">
            <Loader2 size={18} className="animate-spin" />
            <span className="text-sm">搜索中...</span>
          </div>
        )}

        {!loading && query && results?.total === 0 && (
          <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
            <Search size={32} className="mb-3 opacity-30" />
            <p className="text-sm">未找到「{query}」相关结果</p>
          </div>
        )}

        {!loading && results && results.total > 0 && (
          <div className="space-y-5">
            {/* 消息 */}
            {(scope === 'all' || scope === 'messages') && results.messages.length > 0 && (
              <section>
                <h3 className="text-xs text-muted-foreground font-medium mb-2 uppercase tracking-wider">聊天记录</h3>
                {results.messages.map(msg => (
                  <button
                    key={msg.id}
                    onClick={() => handleOpenMessage(msg.conversationId, msg.type)}
                    className="w-full text-left dove-list-item rounded-xl mb-1"
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-medium truncate">{msg.conversationName}</span>
                        <span className="text-[10px] text-muted-foreground ml-2 flex-shrink-0">
                          {new Date(msg.timestamp).toLocaleDateString('zh-CN')}
                        </span>
                      </div>
                      <p className="text-xs text-muted-foreground mt-0.5 truncate">
                        {msg.senderName}: {msg.highlight || msg.content}
                      </p>
                    </div>
                  </button>
                ))}
              </section>
            )}

            {/* 联系人 */}
            {(scope === 'all' || scope === 'users') && results.users.length > 0 && (
              <section>
                <h3 className="text-xs text-muted-foreground font-medium mb-2 uppercase tracking-wider">联系人</h3>
                {results.users.map(user => (
                  <button
                    key={user.id}
                    onClick={() => { openChat(user.id); onClose(); }}
                    className="w-full dove-list-item rounded-xl mb-1"
                  >
                    <DoveAvatar name={user.nickname} id={user.id} avatar={user.avatar} size="md" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium truncate">{user.nickname}</p>
                      <p className="text-xs text-muted-foreground">@{user.username}{user.isBot ? ' · Bot' : ''}</p>
                    </div>
                  </button>
                ))}
              </section>
            )}

            {/* 群组 */}
            {(scope === 'all' || scope === 'groups') && results.groups.length > 0 && (
              <section>
                <h3 className="text-xs text-muted-foreground font-medium mb-2 uppercase tracking-wider">群组</h3>
                {results.groups.map(group => (
                  <button
                    key={group.id}
                    onClick={() => handleOpenMessage(group.id, 'group')}
                    className="w-full dove-list-item rounded-xl mb-1"
                  >
                    <DoveAvatar name={group.name} id={group.id} avatar={group.avatar || ''} size="md" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium truncate">{group.name}</p>
                      <p className="text-xs text-muted-foreground">{group.memberCount} 成员</p>
                    </div>
                  </button>
                ))}
              </section>
            )}

            {/* 频道 */}
            {(scope === 'all' || scope === 'channels') && results.channels.length > 0 && (
              <section>
                <h3 className="text-xs text-muted-foreground font-medium mb-2 uppercase tracking-wider">频道</h3>
                {results.channels.map(ch => (
                  <button
                    key={ch.id}
                    onClick={() => handleOpenMessage(ch.id, 'group')}
                    className="w-full dove-list-item rounded-xl mb-1"
                  >
                    <DoveAvatar name={ch.name} id={ch.id} avatar={ch.avatar || ''} size="md" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium truncate">{ch.name}</p>
                      <p className="text-xs text-muted-foreground">
                        {ch.username ? `@${ch.username}` : ''} · {ch.memberCount} 订阅
                      </p>
                    </div>
                  </button>
                ))}
              </section>
            )}

            {/* 文件 */}
            {(scope === 'all' || scope === 'files') && results.files.length > 0 && (
              <section>
                <h3 className="text-xs text-muted-foreground font-medium mb-2 uppercase tracking-wider">文件</h3>
                {results.files.map(file => (
                  <a
                    key={file.id}
                    href={file.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="w-full dove-list-item rounded-xl mb-1"
                  >
                    <FileText size={20} className="text-dove-green flex-shrink-0" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium truncate">{file.filename}</p>
                      <p className="text-xs text-muted-foreground">{file.type}</p>
                    </div>
                  </a>
                ))}
              </section>
            )}
          </div>
        )}
      </div>
    </motion.div>
  );
}
