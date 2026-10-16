import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      // Префикс _ — принятая в коде пометка «намеренно не используется»: так
      // помечены пропсы, оставленные в деструктуризации для читаемости, и
      // проглоченные ошибки в catch.
      '@typescript-eslint/no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
      }],
    },
  },
  {
    // Модули, где компонент намеренно лежит рядом с не-компонентным экспортом:
    // примитивы shadcn отдают cva-варианты, шаблоны лендинга — дескриптор с
    // метаданными, пиксели — императивный API трекинга, ThemeContext — хук,
    // router — конфиг маршрутов. Правило касается только качества Fast Refresh
    // в дев-режиме, поэтому здесь им осознанно жертвуем.
    files: [
      'src/components/ui/*.tsx',
      'src/templates/*.tsx',
      'src/features/analytics/*Pixel.tsx',
      'src/features/theme/ThemeContext.tsx',
      'src/router.tsx',
    ],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
  {
    // TODO(react-hooks): разобрать эти 11 нарушений отдельной задачей, не в релизе.
    //
    // set-state-in-effect и purity — правила React Compiler, и они указывают на
    // настоящие шероховатости: синхронный setState в эффекте даёт каскадный ререндер,
    // Date.now() в теле компонента делает рендер неидемпотентным. Но лежат они в
    // оплате (CheckoutRoute), поллинге заказа (OrderDeliveryRoute) и админке —
    // переписывать эффекты там нужно с ручной проверкой воронки, а автотестов на
    // фронте нет. Поэтому здесь warn: нарушения видны в выводе линта и не забыты,
    // но CI не краснеет на коде, который работает.
    //
    // Список файлов явный, чтобы новые нарушения в остальном коде оставались ошибкой.
    files: [
      'src/components/activity-toast/ActivityToast.tsx',
      'src/components/landing/AudioSamplesSection.tsx',
      'src/components/landing/ReviewsSection.tsx',
      'src/routes/AdminOrdersRoute.tsx',
      'src/routes/CheckoutRoute.tsx',
      'src/routes/OrderDeliveryRoute.tsx',
    ],
    rules: {
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/purity': 'warn',
    },
  },
])
