/** Never log this URL: its query carries a short-lived login credential. */
export function authenticatedSignalUrl(userId?: string): string | null {
  const token = localStorage.getItem('user_token');
  if (!token) return null;
  const url = new URL('/signal', window.location.origin);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  if (userId) url.searchParams.set('userId', userId);
  url.searchParams.set('token', token);
  return url.toString();
}
