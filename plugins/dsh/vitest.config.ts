import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  resolve: { alias: [{ find: /^@deepseek-ai\/dsh-client-ui-primitives$/, replacement: fileURLToPath(new URL('./tests/native-primitives.ts', import.meta.url)) }] },
  test: {
    include: ['tests/**/*.test.ts', 'tests/**/*.test.tsx'],
    server: { deps: { inline: [/@deepseek-ai\/dsh-client-ui-primitives/] } },
    testTimeout: 30_000,
    hookTimeout: 30_000,
    environment: 'node',
  },
})
