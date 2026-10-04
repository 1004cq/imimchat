type Session = { userId: string; expiresAt: Date; user?: { isBanned: boolean } | null };
export async function authenticateSignal(
  token: string | null, claimedUserId: string | null,
  lookup: (token: string) => Promise<Session | null>,
): Promise<string> {
  if (!token) throw new Error('Unauthorized signal');
  const session = await lookup(token);
  if (!session || session.expiresAt <= new Date() || session.user?.isBanned ||
      claimedUserId && claimedUserId !== session.userId) throw new Error('Unauthorized signal');
  return session.userId;
}

export async function sessionResetSignal(from: string, to: unknown, hasChat: (peer: string) => Promise<boolean>) {
  if (typeof to !== 'string' || !to || to === from || !await hasChat(to)) return null;
  return { type: 'e2ee_session_reset' as const, from, to };
}
