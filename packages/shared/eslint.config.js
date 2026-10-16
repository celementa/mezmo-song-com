import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

// Пакет — только Zod-схемы, общие для фронта и mock-api: ни React, ни браузерных
// глобалов, поэтому плагинов фронтового конфига здесь нет.
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.ts'],
    extends: [js.configs.recommended, tseslint.configs.recommended],
  },
])
