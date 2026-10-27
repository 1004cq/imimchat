import { ensureRuntime, getCachedRuntime, resolveSignalBase } from '@/lib/runtimeConfig';

/** Never log this URL: its query carries a short-lived login credential. */
export function authenticatedSignalUrl(userId?: string): string | null {
  const token = localStorage.getItem('user_token');
  if (!token) return null;

  const stickyKey = userId || localStorage.getItem('user_id') || '';
  const runtime = getCachedRuntime();
  const base = runtime
    ? resolveSignalBase(runtime, stickyKey)
    : (() => {
        const fallback = new URL('/signal', window.location.origin);
        fallback.protocol = fallback.protocol === 'https:' ? 'wss:' : 'ws:';
        return `${fallback.protocol}//${fallback.host}${fallback.pathname}`;
      })();

  const url = new URL(base);
  if (userId) url.searchParams.set('userId', userId);
  url.searchParams.set('token', token);
  return url.toString();
}

/** Prefetch topology then build authenticated signal URL (preferred before connect). */
export async function authenticatedSignalUrlAsync(userId?: string): Promise<string | null> {
  await ensureRuntime();
  return authenticatedSignalUrl(userId);
}
