const FALLBACK_ORIGIN = 'https://cq.je';

/** Current site origin; falls back to the production domain outside the browser. */
export function getPublicOrigin(): string {
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin.replace(/\/$/, '');
  }
  return FALLBACK_ORIGIN;
}

export function publicUrl(path = ''): string {
  const origin = getPublicOrigin();
  if (!path) return origin;
  if (/^https?:\/\//i.test(path)) return path;
  return `${origin}${path.startsWith('/') ? path : `/${path}`}`;
}

export function publicHostPath(path = ''): string {
  try {
    const url = new URL(publicUrl(path));
    return `${url.host}${url.pathname}`.replace(/\/$/, '');
  } catch {
    return publicUrl(path).replace(/^https?:\/\//, '');
  }
}
