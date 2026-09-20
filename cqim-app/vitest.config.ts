import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'node',
    include: [
      'server/**/*.test.ts',
      'client/src/lib/presence.test.ts',
      'client/src/lib/e2ee/**/*.test.ts',
      'client/src/components/VirtualMessageList.test.ts',
    ],
  },
});
