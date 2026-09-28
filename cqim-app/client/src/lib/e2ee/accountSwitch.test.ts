import { describe, expect, it } from 'vitest';
import {
  shouldResetMemoryOnUserChange,
  decideRegistrationAction,
} from './E2EEManager.ts';

/**
 * SPA 不刷新切换账号 / 旧注册迁移的决策逻辑回归测试。
 *
 * 背景：E2EEManager 是单例。SPA 内退出后直接登录另一账号（无页面刷新）时，
 * 内存中的身份密钥/会话必须重置，且 IndexedDB 里的注册信息必须按归属处理：
 * - 无 userId 绑定的老注册 → 补写绑定、保留历史密钥（不能误删）
 * - 归属另一账号的注册 → 清空后重新生成
 */
describe('shouldResetMemoryOnUserChange', () => {
  it('未初始化时不重置', () => {
    expect(shouldResetMemoryOnUserChange(false, 'u1', 'u2')).toBe(false);
    expect(shouldResetMemoryOnUserChange(false, null, 'u2')).toBe(false);
  });

  it('同账号重复初始化不重置', () => {
    expect(shouldResetMemoryOnUserChange(true, 'u1', 'u1')).toBe(false);
  });

  it('已初始化且登录用户变化时重置（SPA 切账号）', () => {
    expect(shouldResetMemoryOnUserChange(true, 'u1', 'u2')).toBe(true);
  });

  it('已初始化但退出登录（用户变 null）时重置', () => {
    expect(shouldResetMemoryOnUserChange(true, 'u1', null)).toBe(true);
  });
});

describe('decideRegistrationAction', () => {
  it('无本地注册 → generate', () => {
    expect(decideRegistrationAction(null, 'u1')).toBe('generate');
    expect(decideRegistrationAction(null, null)).toBe('generate');
  });

  it('老注册无 userId 绑定 → migrate-bind（保留密钥，只补绑定）', () => {
    expect(decideRegistrationAction({} as any, 'u1')).toBe('migrate-bind');
    expect(decideRegistrationAction({ userId: undefined } as any, 'u1')).toBe('migrate-bind');
    expect(decideRegistrationAction({ userId: '' } as any, 'u1')).toBe('migrate-bind');
  });

  it('未登录时老注册无绑定 → generate 兜底（不误删）', () => {
    // currentUserId 为 null 且 existing 无 userId：migrate-bind 需要 currentUserId，
    // 走 load 分支（原逻辑：existing && !currentUserId → 加载）
    expect(decideRegistrationAction({} as any, null)).toBe('load');
  });

  it('注册归属当前用户 → load', () => {
    expect(decideRegistrationAction({ userId: 'u1' }, 'u1')).toBe('load');
  });

  it('注册归属另一账号 → clear-regenerate', () => {
    expect(decideRegistrationAction({ userId: 'u1' }, 'u2')).toBe('clear-regenerate');
  });

  it('owner 明确不同时才清空：空字符串 userId 不视为有效归属', () => {
    // userId '' 为 falsy → migrate-bind 而非 load，避免把别人的空绑定当成自己
    expect(decideRegistrationAction({ userId: '' }, 'u1')).toBe('migrate-bind');
  });
});
