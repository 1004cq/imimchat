import { createHash, randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import prisma from './db.js';

export const MEDIA_KINDS = ['image', 'voice', 'video', 'sticker', 'file'] as const;
export type MediaKind = (typeof MEDIA_KINDS)[number];

const MAX_BYTES: Record<MediaKind, number> = {
  image: 15 * 1024 * 1024,
  voice: 20 * 1024 * 1024,
  video: Number(process.env.MEDIA_MAX_VIDEO_BYTES || 100 * 1024 * 1024),
  sticker: 8 * 1024 * 1024,
  file: 30 * 1024 * 1024,
};

export function mediaRoot() {
  return process.env.MEDIA_ROOT || path.resolve(process.cwd(), 'data/media');
}

export function isMediaKind(v: unknown): v is MediaKind {
  return typeof v === 'string' && (MEDIA_KINDS as readonly string[]).includes(v);
}

export async function saveLocalMedia(opts: {
  ownerId: string;
  kind: MediaKind;
  buffer: Buffer;
  mime: string;
  filename?: string;
  width?: number;
  height?: number;
  durationMs?: number;
  posterMediaId?: string;
}) {
  const max = MAX_BYTES[opts.kind];
  if (opts.buffer.length > max) {
    throw new Error(`file_too_large:${opts.kind}:${max}`);
  }
  const id = randomBytes(12).toString('hex');
  const year = String(new Date().getUTCFullYear());
  const rel = path.join(opts.kind, year, id);
  const absDir = path.join(mediaRoot(), opts.kind, year);
  await mkdir(absDir, { recursive: true });
  const absFile = path.join(absDir, id);
  await writeFile(absFile, opts.buffer);
  const sha256 = createHash('sha256').update(opts.buffer).digest('hex');
  const publicPath = `/api/media/${id}`;
  const row = await prisma.mediaFile.create({
    data: {
      id,
      userId: opts.ownerId,
      type: opts.kind,
      kind: opts.kind,
      url: publicPath,
      publicPath,
      diskPath: rel,
      filename: opts.filename || null,
      mime: opts.mime || 'application/octet-stream',
      size: opts.buffer.length,
      width: opts.width ?? null,
      height: opts.height ?? null,
      durationMs: opts.durationMs ?? null,
      posterMediaId: opts.posterMediaId ?? null,
      sha256,
    },
  });
  return row;
}

export async function resolveLocalMedia(id: string) {
  const row = await prisma.mediaFile.findUnique({ where: { id } });
  if (!row?.diskPath) return null;
  return {
    row,
    absPath: path.join(mediaRoot(), row.diskPath),
  };
}
