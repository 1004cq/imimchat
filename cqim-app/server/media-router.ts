import { Router, Request, Response } from 'express';
import { createReadStream } from 'node:fs';
import { stat } from 'node:fs/promises';
import { userAuth } from './auth.js';
import { isMediaKind, resolveLocalMedia, saveLocalMedia, type MediaKind } from './media-local.js';

const router = Router();

router.post('/upload', userAuth, async (req: Request, res: Response) => {
  try {
    const body = req.body || {};
    const kind = (body.kind || body.type || 'file') as string;
    if (!isMediaKind(kind)) {
      return res.status(400).json({ error: 'invalid_kind', allow: ['image', 'voice', 'video', 'sticker', 'file'] });
    }
    const b64 = body.data || body.file || body.content;
    if (typeof b64 !== 'string' || b64.length < 8) {
      return res.status(400).json({ error: 'missing_data' });
    }
    const raw = b64.includes(',') ? b64.slice(b64.indexOf(',') + 1) : b64;
    const buffer = Buffer.from(raw, 'base64');
    const row = await saveLocalMedia({
      ownerId: (req as Request & { user?: { id: string } }).user!.id,
      kind: kind as MediaKind,
      buffer,
      mime: body.mime || body.mimeType || 'application/octet-stream',
      filename: body.filename || body.name,
      width: body.width ? Number(body.width) : undefined,
      height: body.height ? Number(body.height) : undefined,
      durationMs: body.durationMs ? Number(body.durationMs) : undefined,
      posterMediaId: body.posterMediaId || undefined,
    });
    return res.json({
      id: row.id,
      kind: row.kind || row.type,
      url: row.publicPath || row.url,
      size: row.size,
      mime: row.mime,
    });
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : 'upload_failed';
    if (String(msg).startsWith('file_too_large')) {
      return res.status(413).json({ error: msg });
    }
    console.error('[media] upload', e);
    return res.status(500).json({ error: 'upload_failed' });
  }
});

router.get('/:id', userAuth, async (req: Request, res: Response) => {
  const found = await resolveLocalMedia(String(req.params.id));
  if (!found) return res.status(404).json({ error: 'not_found' });
  try {
    const st = await stat(found.absPath);
    res.setHeader('Content-Type', found.row.mime || 'application/octet-stream');
    res.setHeader('Content-Length', String(st.size));
    res.setHeader('Cache-Control', 'private, max-age=86400');
    createReadStream(found.absPath).pipe(res);
  } catch {
    return res.status(404).json({ error: 'missing_file' });
  }
});

export default router;
