import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { VirtualMessageList } from './VirtualMessageList';

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

  it('renders an empty chat without throwing ReferenceError', () => {
    expect(() =>
      renderToString(
        createElement(VirtualMessageList, {
          messages: [],
          currentUserId: 'user-1',
          chatId: 'chat-new',
        }),
      ),
    ).not.toThrow();
  });

  it('renders existing messages without throwing ReferenceError', () => {
    const html = renderToString(
      createElement(VirtualMessageList, {
        messages: [
          {
            id: 'm1',
            senderId: 'user-2',
            senderName: 'Ada',
            content: 'hello',
            timestamp: 1_700_000_000_000,
          },
        ],
        currentUserId: 'user-1',
        chatId: 'chat-existing',
      }),
    );
    expect(html).toContain('hello');
  });
});
