import { Router, Request, Response } from 'express';
import busboy from 'busboy';
import { userAuth } from './auth.js';
import { isMediaKind, resolveMedia, saveMedia, type MediaKind } from './media-storage.js';

const router = Router();

type AuthenticatedRequest = Request & { user?: { id: string } };

router.post('/upload', userAuth, async (req: AuthenticatedRequest, res: Response) => {
  try {
    const body = req.body || {};
    const requestedKind = body.kind || body.type || body.mediaType || 'file';
    const kind = (requestedKind === 'audio' ? 'voice' : requestedKind) as string;
    if (!isMediaKind(kind)) {
      return res.status(400).json({ error: 'invalid_kind', allow: ['image', 'voice', 'video', 'sticker', 'file'] });
    }
    const b64 = body.data || body.dataBase64 || body.file || body.content;
    if (typeof b64 !== 'string' || b64.length < 8) {
      return res.status(400).json({ error: 'missing_data' });
    }
    const raw = b64.includes(',') ? b64.slice(b64.indexOf(',') + 1) : b64;
    const buffer = Buffer.from(raw, 'base64');
    const row = await saveMedia({
      ownerId: req.user!.id,
      kind: kind as MediaKind,
      buffer,
      mime: body.mime || body.mimeType || 'application/octet-stream',
      filename: body.filename || body.name,
      width: body.width ? Number(body.width) : undefined,
      height: body.height ? Number(body.height) : undefined,
      durationMs: body.durationMs ? Number(body.durationMs) : undefined,
      posterMediaId: body.posterMediaId || undefined,
    });
    return res.json({ id: row.id, kind: row.kind || row.type, url: row.publicPath || row.url, size: row.size, mime: row.mime, storage: 'minio' });
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : 'upload_failed';
    if (String(msg).startsWith('file_too_large')) return res.status(413).json({ error: msg });
    console.error('[media] MinIO upload', e);
    return res.status(500).json({ error: 'upload_failed' });
  }
});

router.post('/upload-form', userAuth, async (req: AuthenticatedRequest, res: Response) => {
  try {
    const createBusboy = (busboy as any).default || busboy;
    const bb = createBusboy({ headers: req.headers, limits: { fileSize: 200 * 1024 * 1024 } });
    let file: Buffer | null = null;
    let filename = 'upload';
    let mime = 'application/octet-stream';
    let kind = 'file';
    let truncated = false;
    bb.on('field', (name: string, value: string) => {
      if (name === 'kind' || name === 'mediaType') kind = value === 'audio' ? 'voice' : value;
    });
    bb.on('file', (_name: string, stream: any, info: any) => {
      filename = info.filename || filename;
      mime = info.mimeType || mime;
      const chunks: Buffer[] = [];
      stream.on('data', (chunk: Buffer) => chunks.push(chunk));
      stream.on('limit', () => { truncated = true; });
      stream.on('end', () => { file = Buffer.concat(chunks); });
    });
    bb.on('finish', async () => {
      try {
        if (truncated) return res.status(413).json({ error: 'file_too_large' });
        if (!file || !isMediaKind(kind)) return res.status(400).json({ error: 'invalid_upload' });
        const row = await saveMedia({ ownerId: req.user!.id, kind: kind as MediaKind, buffer: file, mime, filename });
        return res.json({ ok: true, id: row.id, kind: row.kind, url: row.publicPath || row.url, fileName: row.filename, storage: 'minio', size: row.size, mime: row.mime });
      } catch (error) {
        console.error('[media] MinIO form upload', error);
        return res.status(500).json({ error: 'upload_failed' });
      }
    });
    bb.on('error', () => res.status(400).json({ error: 'invalid_multipart' }));
    req.pipe(bb);
  } catch (error) {
    console.error('[media] MinIO form parser', error);
    return res.status(500).json({ error: 'upload_failed' });
  }
});

// The opaque MediaFile id acts as the capability for browser media elements;
// no Authorization header is available on ordinary <img>/<audio>/<video> loads.
router.get('/:id', async (req: Request, res: Response) => {
  try {
    const found = await resolveMedia(String(req.params.id));
    if (!found) return res.status(404).json({ error: 'not_found' });
    res.setHeader('Content-Type', found.row.mime || 'application/octet-stream');
    res.setHeader('Content-Length', String(found.size));
    res.setHeader('Cache-Control', 'private, max-age=86400');
    found.object.on('error', () => { if (!res.headersSent) res.status(404).end(); });
    return found.object.pipe(res);
  } catch {
    return res.status(404).json({ error: 'missing_object' });
  }
});

export default router;
