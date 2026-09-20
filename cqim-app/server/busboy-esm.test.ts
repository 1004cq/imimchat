import { describe, it, expect } from 'vitest';
import { Readable } from 'stream';
import busboy from 'busboy';

function createBusboy(headers: Record<string, string>) {
  const factory = (busboy as any).default || busboy;
  return factory({ headers, limits: { fileSize: 200 * 1024 * 1024 } });
}

describe('busboy ESM import', () => {
  it('loads as a callable factory without require()', () => {
    expect(typeof ((busboy as any).default || busboy)).toBe('function');
  });

  it('parses multipart form-data used by /upload-form', async () => {
    const boundary = '----testboundary';
    const body = Buffer.from(
      `--${boundary}\r\n` +
        `Content-Disposition: form-data; name="mediaType"\r\n\r\n` +
        `image\r\n` +
        `--${boundary}\r\n` +
        `Content-Disposition: form-data; name="file"; filename="pic.png"\r\n` +
        `Content-Type: image/png\r\n\r\n` +
        `PNGDATA\r\n` +
        `--${boundary}--\r\n`,
    );

    const bb = createBusboy({
      'content-type': `multipart/form-data; boundary=${boundary}`,
    });
    let kind = '';
    let file: Buffer | null = null;
    let filename = '';
    let mime = '';

    await new Promise<void>((resolve, reject) => {
      bb.on('field', (name: string, value: string) => {
        if (name === 'kind' || name === 'mediaType') kind = value === 'audio' ? 'voice' : value;
      });
      bb.on('file', (_name: string, stream: any, info: any) => {
        filename = info.filename || filename;
        mime = info.mimeType || mime;
        const chunks: Buffer[] = [];
        stream.on('data', (chunk: Buffer) => chunks.push(chunk));
        stream.on('end', () => {
          file = Buffer.concat(chunks);
        });
      });
      bb.on('finish', () => resolve());
      bb.on('close', () => resolve());
      bb.on('error', reject);
      Readable.from(body).pipe(bb);
    });

    expect(kind).toBe('image');
    expect(filename).toBe('pic.png');
    expect(mime).toBe('image/png');
    expect(file?.toString()).toBe('PNGDATA');
  });
});
