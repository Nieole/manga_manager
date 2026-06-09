/**
 * 业务说明：本文件是前端代码质量配置，负责约束 TypeScript、React Hooks 和 Vite 热更新相关的静态检查。
 * 它不直接参与运行时业务，但会影响资料库、阅读器、设置页等页面在提交前能否暴露明显的组件和 hook 风险。
 * 维护时应关注规则开关是否服务于现有代码结构，避免为了通过 lint 放宽真正会导致用户流程异常的检查。
 */

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
      'react-hooks/config': 'off',
      'react-hooks/error-boundaries': 'off',
      'react-hooks/gating': 'off',
      'react-hooks/globals': 'off',
      'react-hooks/immutability': 'off',
      'react-hooks/incompatible-library': 'off',
      'react-hooks/preserve-manual-memoization': 'off',
      'react-hooks/purity': 'off',
      'react-hooks/refs': 'off',
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/set-state-in-render': 'off',
      'react-hooks/static-components': 'off',
      'react-hooks/unsupported-syntax': 'off',
      'react-hooks/use-memo': 'off',
    },
  },
  {
    files: ['src/**/*Provider.tsx', 'src/**/*Context.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
])
