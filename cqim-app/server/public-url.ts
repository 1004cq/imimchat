const DEFAULT_PUBLIC_BASE_URL = 'https://cq.je';

/** Canonical public origin for invite links, OG tags, and push assets. */
export function getPublicBaseUrl(): string {
  const raw = process.env.PUBLIC_BASE_URL?.trim();
  if (raw) return raw.replace(/\/$/, '');
  return DEFAULT_PUBLIC_BASE_URL;
}

export function publicUrl(path = ''): string {
  const base = getPublicBaseUrl();
  if (!path) return base;
  if (/^https?:\/\//i.test(path)) return path;
  return `${base}${path.startsWith('/') ? path : `/${path}`}`;
}
