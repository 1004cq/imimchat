/**
 * 搜索服务 — 架构图「消息 / 群 / 频道 / 文件」全文检索
 *
 * 当前实现：Prisma + SQLite LIKE 查询（开发/中小规模）
 * 生产扩展：设置 ELASTICSEARCH_URL 后自动切换 ES 后端
 */

import { Router, Request, Response } from 'express';
import prisma from './db.js';
import { avatarToProxy } from './cos-signer.js';

const searchRouter = Router();

export type SearchScope = 'all' | 'messages' | 'users' | 'groups' | 'channels' | 'files';

export interface SearchResult {
  messages: Array<{
    id: string;
    type: 'private' | 'group';
    conversationId: string;
    conversationName: string;
    senderId: string;
    senderName: string;
    content: string;
    msgType: string;
    timestamp: string;
    highlight?: string;
  }>;
  users: Array<{
    id: string;
    username: string;
    nickname: string;
    avatar: string;
    isBot: boolean;
  }>;
  groups: Array<{
    id: string;
    name: string;
    username: string | null;
    type: string;
    memberCount: number;
    avatar: string | null;
  }>;
  channels: Array<{
    id: string;
    name: string;
    username: string | null;
    memberCount: number;
    avatar: string | null;
  }>;
  files: Array<{
    id: string;
    filename: string;
    type: string;
    url: string;
    size: number | null;
    createdAt: string;
  }>;
  total: number;
}

function highlightText(text: string, query: string): string {
  if (!text || !query) return text;
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  if (idx === -1) return text.slice(0, 100);
  const start = Math.max(0, idx - 20);
  const end = Math.min(text.length, idx + query.length + 40);
  const snippet = (start > 0 ? '...' : '') + text.slice(start, end) + (end < text.length ? '...' : '');
  return snippet;
}

async function searchPrivateMessages(query: string, userId: string, limit: number) {
  const chats = await prisma.chat.findMany({
    where: {
      OR: [{ participantA: userId }, { participantB: userId }],
    },
    select: { id: true, participantA: true, participantB: true },
  });
  const chatIds = chats.map(c => c.id);
  if (chatIds.length === 0) return [];

  const messages = await prisma.privateMessage.findMany({
    where: {
      chatId: { in: chatIds },
      isRevoked: false,
      content: { contains: query },
    },
    orderBy: { createdAt: 'desc' },
    take: limit,
    include: {
      chat: { select: { participantA: true, participantB: true } },
    },
  });

  const peerIds = new Set<string>();
  for (const m of messages) {
    const peerId = m.chat.participantA === userId ? m.chat.participantB : m.chat.participantA;
    peerIds.add(peerId);
    peerIds.add(m.senderId);
  }
  const users = await prisma.user.findMany({
    where: { id: { in: [...peerIds] } },
    select: { id: true, nickname: true, username: true },
  });
  const userMap = new Map(users.map(u => [u.id, u]));

  return messages.map(m => {
    const peerId = m.chat.participantA === userId ? m.chat.participantB : m.chat.participantA;
    const peer = userMap.get(peerId);
    const sender = userMap.get(m.senderId);
    return {
      id: m.id,
      type: 'private' as const,
      conversationId: m.chatId,
      conversationName: peer?.nickname || peer?.username || peerId,
      senderId: m.senderId,
      senderName: sender?.nickname || sender?.username || m.senderId,
      content: m.content,
      msgType: m.msgType,
      timestamp: m.createdAt.toISOString(),
      highlight: highlightText(m.content, query),
    };
  });
}

async function searchGroupMessages(query: string, userId: string, limit: number) {
  const memberships = await prisma.groupMember.findMany({
    where: { userId },
    select: { groupId: true },
  });
  const groupIds = memberships.map(m => m.groupId);
  if (groupIds.length === 0) return [];

  const messages = await prisma.groupMessage.findMany({
    where: {
      groupId: { in: groupIds },
      isRevoked: false,
      content: { contains: query },
    },
    orderBy: { createdAt: 'desc' },
    take: limit,
  });

  const groups = await prisma.group.findMany({
    where: { id: { in: groupIds } },
    select: { id: true, name: true },
  });
  const groupMap = new Map(groups.map(g => [g.id, g]));

  return messages.map(m => ({
    id: m.id,
    type: 'group' as const,
    conversationId: m.groupId,
    conversationName: groupMap.get(m.groupId)?.name || m.groupId,
    senderId: m.senderId,
    senderName: m.senderName || m.senderId,
    content: m.content,
    msgType: m.msgType,
    timestamp: m.createdAt.toISOString(),
    highlight: highlightText(m.content, query),
  }));
}

async function searchUsers(query: string, limit: number) {
  const users = await prisma.user.findMany({
    where: {
      isBanned: false,
      OR: [
        { username: { contains: query } },
        { nickname: { contains: query } },
        { phone: { contains: query } },
      ],
    },
    take: limit,
    select: { id: true, username: true, nickname: true, avatar: true, isBot: true },
  });
  return users.map(u => ({
    id: u.id,
    username: u.username,
    nickname: u.nickname || u.username,
    avatar: avatarToProxy(u.avatar) || '',
    isBot: u.isBot,
  }));
}

async function searchGroups(query: string, limit: number) {
  const groups = await prisma.group.findMany({
    where: {
      type: { in: ['normal', 'super'] },
      OR: [
        { name: { contains: query } },
        { username: { contains: query } },
      ],
      isPublic: true,
    },
    take: limit,
    select: { id: true, name: true, username: true, type: true, memberCount: true, avatar: true },
  });
  return groups.map(g => ({
    id: g.id,
    name: g.name,
    username: g.username,
    type: g.type,
    memberCount: g.memberCount,
    avatar: avatarToProxy(g.avatar),
  }));
}

async function searchChannels(query: string, limit: number) {
  const channels = await prisma.group.findMany({
    where: {
      type: 'channel',
      OR: [
        { name: { contains: query } },
        { username: { contains: query } },
      ],
    },
    take: limit,
    select: { id: true, name: true, username: true, memberCount: true, avatar: true },
  });
  return channels.map(c => ({
    id: c.id,
    name: c.name,
    username: c.username,
    memberCount: c.memberCount,
    avatar: avatarToProxy(c.avatar),
  }));
}

async function searchFiles(query: string, userId: string, limit: number) {
  const files = await prisma.mediaFile.findMany({
    where: {
      OR: [
        { userId },
        { userId: null },
      ],
      filename: { contains: query },
    },
    orderBy: { createdAt: 'desc' },
    take: limit,
    select: { id: true, filename: true, type: true, url: true, size: true, createdAt: true },
  });
  return files.map(f => ({
    id: f.id,
    filename: f.filename || '未命名文件',
    type: f.type,
    url: f.url,
    size: f.size,
    createdAt: f.createdAt.toISOString(),
  }));
}

/** 全局搜索 */
export async function globalSearch(
  query: string,
  userId: string,
  scope: SearchScope = 'all',
  limit = 20,
): Promise<SearchResult> {
  const q = query.trim();
  if (!q || q.length < 1) {
    return { messages: [], users: [], groups: [], channels: [], files: [], total: 0 };
  }

  const perScope = Math.ceil(limit / 2);
  const tasks: Promise<void>[] = [];
  const result: SearchResult = {
    messages: [],
    users: [],
    groups: [],
    channels: [],
    files: [],
    total: 0,
  };

  if (scope === 'all' || scope === 'messages') {
    tasks.push(
      Promise.all([
        searchPrivateMessages(q, userId, perScope),
        searchGroupMessages(q, userId, perScope),
      ]).then(([privateMsgs, groupMsgs]) => {
        result.messages = [...privateMsgs, ...groupMsgs]
          .sort((a, b) => b.timestamp.localeCompare(a.timestamp))
          .slice(0, limit);
      }),
    );
  }

  if (scope === 'all' || scope === 'users') {
    tasks.push(searchUsers(q, limit).then(users => { result.users = users; }));
  }

  if (scope === 'all' || scope === 'groups') {
    tasks.push(searchGroups(q, limit).then(groups => { result.groups = groups; }));
  }

  if (scope === 'all' || scope === 'channels') {
    tasks.push(searchChannels(q, limit).then(channels => { result.channels = channels; }));
  }

  if (scope === 'all' || scope === 'files') {
    tasks.push(searchFiles(q, userId, limit).then(files => { result.files = files; }));
  }

  await Promise.all(tasks);
  result.total =
    result.messages.length +
    result.users.length +
    result.groups.length +
    result.channels.length +
    result.files.length;

  return result;
}

/** MQ 异步索引钩子（预留 ES 扩展点） */
export async function indexMessageAsync(payload: {
  messageId?: string;
  groupId?: string;
  chatId?: string;
  content?: string;
}): Promise<void> {
  const esUrl = process.env.ELASTICSEARCH_URL;
  if (!esUrl) return; // 未配置 ES 时跳过

  // 预留：POST ${esUrl}/messages/_doc
  console.log(`[Search] ES 索引预留: messageId=${payload.messageId}`);
}

// ============ API 路由 ============

/** GET /api/search?q=&scope=all&limit=20 */
searchRouter.get('/', async (req: Request, res: Response) => {
  try {
    const { q, scope = 'all', limit = '20', userId } = req.query as Record<string, string>;
    if (!q) {
      return res.status(400).json({ error: '缺少搜索关键词 q' });
    }
    if (!userId) {
      return res.status(400).json({ error: '缺少 userId' });
    }
    const result = await globalSearch(
      q,
      userId,
      scope as SearchScope,
      Math.min(parseInt(limit) || 20, 50),
    );
    res.json(result);
  } catch (err: any) {
    res.status(500).json({ error: err.message });
  }
});

export default searchRouter;
