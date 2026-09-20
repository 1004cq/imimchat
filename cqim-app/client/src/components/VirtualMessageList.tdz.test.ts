import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const src = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'VirtualMessageList.tsx'), 'utf8');

describe('VirtualMessageList prevLengthRef TDZ', () => {
  it('initializes prevLengthRef to 0 before safeMessages exists', () => {
    const fnIdx = src.indexOf('function VirtualMessageList');
    expect(fnIdx).toBeGreaterThan(-1);
    const body = src.slice(fnIdx);
    const prevDecl = body.indexOf('const prevLengthRef');
    const safeDecl = body.indexOf('const safeMessages');
    expect(prevDecl).toBeGreaterThan(-1);
    expect(safeDecl).toBeGreaterThan(-1);

    const init = body.match(/const prevLengthRef = useRef\(([^)]+)\)/);
    expect(init?.[1].replace(/\s+/g, '')).toBe('0');
    expect(init?.[1]).not.toContain('safeMessages');
  });

  it('still updates prevLengthRef from safeMessages.length after the list is built', () => {
    expect(src).toContain('safeMessages.length - prevLengthRef.current');
    expect(src).toContain('prevLengthRef.current = safeMessages.length');
  });
});
