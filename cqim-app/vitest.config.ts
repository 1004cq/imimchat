import path from 'node:path';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, 'client/src'),
    },
  },
  test: {
    environment: 'node',
    include: [
      'server/**/*.test.ts',
      'client/src/lib/presence.test.ts',
      'client/src/lib/store.authme.test.ts',
      'client/src/lib/e2ee/**/*.test.ts',
      'client/src/components/VirtualMessageList.test.ts',
      'client/src/components/moments/utils.test.ts',
    ],
  },
});
