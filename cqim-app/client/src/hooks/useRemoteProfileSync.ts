/**
 * useRemoteProfileSync
 *
 * 订阅全局事件 `cqim:remote-user-profile-updated`，当好友/群成员更新头像、
 * 昵称等资料时，触发回调以刷新相关 UI 组件（如会话列表、群成员列表）。
 */
import { useEffect, useRef } from 'react';

export interface RemoteProfileUpdate {
  userId: string;
  nickname?: string;
  avatar?: string;
  username?: string;
  bio?: string;
  backgroundUrl?: string;
  updatedAt?: number;
}

/** 为头像 URL 附加版本参数，避免 CDN/浏览器缓存旧图 */
export function avatarWithVersion(avatar: string | undefined, updatedAt?: number): string | undefined {
  if (!avatar) return avatar;
  if (!updatedAt) return avatar;
  const sep = avatar.includes('?') ? '&' : '?';
  return `${avatar}${sep}v=${updatedAt}`;
}

/**
 * 注册远程用户资料更新回调。
 * @param onUpdate - 当收到远程资料更新时调用
 * @param filterUserIds - 可选，只关注特定用户的更新（传入 userId 数组）
 */
export function useRemoteProfileSync(
  onUpdate: (update: RemoteProfileUpdate) => void,
  filterUserIds?: string[]
) {
  const onUpdateRef = useRef(onUpdate);
  onUpdateRef.current = onUpdate;

  const filterRef = useRef(filterUserIds);
  filterRef.current = filterUserIds;

  const lastAppliedRef = useRef<Map<string, number>>(new Map());

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<RemoteProfileUpdate>).detail;
      if (!detail || !detail.userId) return;

      const filter = filterRef.current;
      if (filter && filter.length > 0 && !filter.includes(detail.userId)) return;

      const prev = lastAppliedRef.current.get(detail.userId) ?? 0;
      const nextTs = detail.updatedAt ?? 0;
      if (nextTs > 0 && nextTs <= prev) return;
      if (nextTs > 0) lastAppliedRef.current.set(detail.userId, nextTs);

      const normalized: RemoteProfileUpdate = {
        ...detail,
        avatar: avatarWithVersion(detail.avatar, detail.updatedAt),
      };
      onUpdateRef.current(normalized);
    };

    window.addEventListener('cqim:remote-user-profile-updated', handler);
    return () => {
      window.removeEventListener('cqim:remote-user-profile-updated', handler);
    };
  }, []);
}

export function mergeProfileUpdate<T extends { id?: string; userId?: string; name?: string; avatar?: string }>(
  item: T,
  update: RemoteProfileUpdate
): T {
  const itemUserId = item.id || item.userId || '';
  if (itemUserId !== update.userId) return item;

  const changes: Partial<T> = {} as Partial<T>;
  if (update.nickname !== undefined) (changes as any).name = update.nickname;
  if (update.avatar !== undefined) (changes as any).avatar = update.avatar;

  return { ...item, ...changes };
}
