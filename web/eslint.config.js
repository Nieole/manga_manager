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
      // 统一走 src/api/client 的 apiClient 实例（含鉴权 X-API-Token 与 locale 头拦截器）。
      // 直接 import axios 的值会绕过这些横切逻辑，故禁止；type 导入（如 AxiosResponse）无运行时、放行。
      // 基础设施文件在下方 override 中豁免。用 typescript-eslint 版本以支持 allowTypeImports。
      'no-restricted-imports': 'off',
      '@typescript-eslint/no-restricted-imports': ['error', {
        paths: [{
          name: 'axios',
          allowTypeImports: true,
          message: '请从 src/api/client 导入 apiClient / isAxiosError / isCancel，不要直接 import axios 的值（否则绕过统一实例的鉴权与 locale 拦截器）。',
        }],
      }],
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
  {
    // 基础设施文件：client 需 axios.create/isAxiosError，apiAuth 需 axios.interceptors；豁免 axios 禁令。
    files: ['src/api/client.ts', 'src/utils/apiAuth.ts'],
    rules: {
      '@typescript-eslint/no-restricted-imports': 'off',
    },
  },
])
