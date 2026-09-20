import React from 'react';
import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { DoveAvatar } from './DoveAvatar.tsx';

describe('DoveAvatar', () => {
  it('renders an img when an avatar URL is present', () => {
    const html = renderToString(
      <DoveAvatar name="Qing" id="1" avatar="/api/media/0670b9a72a7f207e7fb977f2" size={40} />,
    );
    expect(html).toContain('<img');
    expect(html).toContain('/api/media/0670b9a72a7f207e7fb977f2');
  });

  it('renders initials when the avatar URL is empty', () => {
    const html = renderToString(
      <DoveAvatar name="User0" id="0" avatar="" size={40} />,
    );
    expect(html).not.toContain('<img');
    expect(html).toContain('U');
  });
});
