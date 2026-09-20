import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const source = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), 'VirtualMessageList.tsx'),
  'utf8',
);

describe('VirtualMessageList temporal dead zone', () => {
  it('initializes prevLengthRef before safeMessages is declared', () => {
    expect(source).not.toMatch(/const prevLengthRef = useRef\(safeMessages\.length\)/);
    expect(source).toMatch(/const prevLengthRef = useRef\(0\)/);

    const prevDecl = source.indexOf('const prevLengthRef = useRef(0)');
    const safeDecl = source.indexOf('const safeMessages = useMemo(');
    expect(prevDecl).toBeGreaterThan(-1);
    expect(safeDecl).toBeGreaterThan(-1);
    expect(prevDecl).toBeLessThan(safeDecl);
  });
});
