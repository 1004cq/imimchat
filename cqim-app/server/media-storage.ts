import { createHash, randomBytes } from 'node:crypto';
import { Readable } from 'node:stream';
import { Client } from 'minio';
import { prisma } from './db.js';

export const MEDIA_KINDS = ['image', 'voice', 'video', 'sticker', 'file'] as const;
export type MediaKind = (typeof MEDIA_KINDS)[number];

const MAX_BYTES: Record<MediaKind, number> = {
  image: 15 * 1024 * 1024,
  voice: 20 * 1024 * 1024,
  video: Number(process.env.MEDIA_MAX_VIDEO_BYTES || 100 * 1024 * 1024),
  sticker: 8 * 1024 * 1024,
  file: 30 * 1024 * 1024,
};

const minio = new Client({
  endPoint: process.env.MINIO_ENDPOINT || 'minio',
  port: Number(process.env.MINIO_PORT || 9000),
  useSSL: String(process.env.MINIO_USE_SSL || 'false') === 'true',
  accessKey: process.env.MINIO_ROOT_USER || 'cqimminio',
  secretKey: process.env.MINIO_ROOT_PASSWORD || 'cqimio-secret-change-me',
  region: process.env.MINIO_REGION || 'us-east-1',
});

export function mediaBucket() {
  return process.env.MINIO_BUCKET || 'cqim-media';
}

export function isMediaKind(value: unknown): value is MediaKind {
  return typeof value === 'string' && (MEDIA_KINDS as readonly string[]).includes(value);
}

export async function ensureMediaBucket() {
  const bucket = mediaBucket();
  if (!(await minio.bucketExists(bucket))) {
    await minio.makeBucket(bucket, process.env.MINIO_REGION || 'us-east-1');
  }
}

function objectKey(kind: MediaKind, id: string) {
  return `media/${kind}/${new Date().getUTCFullYear()}/${id}`;
}

export async function saveMedia(opts: {
  ownerId: string | null;
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
  await ensureMediaBucket();
  const id = randomBytes(12).toString('hex');
  const key = objectKey(opts.kind, id);
  const mime = opts.mime || 'application/octet-stream';
  const sha256 = createHash('sha256').update(opts.buffer).digest('hex');
  await minio.putObject(mediaBucket(), key, opts.buffer, opts.buffer.length, {
    'Content-Type': mime,
    'X-Amz-Meta-Original-Filename': opts.filename || '',
  });
  try {
    return await prisma.mediaFile.create({
      data: {
        id,
        userId: opts.ownerId,
        type: opts.kind,
        kind: opts.kind,
        url: `/api/media/${id}`,
        publicPath: `/api/media/${id}`,
        diskPath: key,
        filename: opts.filename || null,
        mime,
        size: opts.buffer.length,
        width: opts.width ?? null,
        height: opts.height ?? null,
        durationMs: opts.durationMs ?? null,
        posterMediaId: opts.posterMediaId ?? null,
        sha256,
      },
    });
  } catch (error) {
    await minio.removeObject(mediaBucket(), key).catch(() => undefined);
    throw error;
  }
}

export async function resolveMedia(id: string) {
  const row = await prisma.mediaFile.findUnique({ where: { id } });
  if (!row?.diskPath) return null;
  const object = await minio.getObject(mediaBucket(), row.diskPath);
  const stat = await minio.statObject(mediaBucket(), row.diskPath);
  return { row, object: object as Readable, size: stat.size };
}

export async function resolveLegacyMedia(filename: string) {
  const object = await minio.getObject(mediaBucket(), `legacy/${filename}`);
  const stat = await minio.statObject(mediaBucket(), `legacy/${filename}`);
  return { object: object as Readable, size: stat.size };
}
