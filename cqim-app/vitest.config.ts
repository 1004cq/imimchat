import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'node',
    include: ['server/**/*.test.ts', 'client/src/lib/presence.test.ts', 'client/src/components/moments/momentsHeader.test.ts'],
  },
});
