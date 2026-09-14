/** Canonical Tencent TRTC SDKAppID for CQIM. */
export const CANONICAL_TRTC_SDK_APP_ID = 1600159677;

/**
 * Resolve TRTC SDKAppID from env.
 * Missing or invalid values return null so the caller can fail loudly.
 * There is no silent default and no retired-app fallback.
 */
export function resolveTrtcSdkAppId(raw: string | undefined | null): number | null {
  if (raw == null) return null;
  const trimmed = String(raw).trim();
  if (!trimmed) return null;
  const n = Number(trimmed);
  if (!Number.isFinite(n) || !Number.isInteger(n) || n <= 0) return null;
  return n;
}
