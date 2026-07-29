/**
 * 业务说明：本文件是前端单元测试配置，负责用 Vitest 运行 src 下的测试。
 *
 * 默认仍是 node 环境：绝大多数用例是纯函数（i18n 归一化、阅读进度计算、分页参数等），
 * 让它们跑在 node 下最快也最稳。需要 DOM 的用例（组件、hook）在文件顶部加一行
 * `@vitest-environment jsdom` docblock 单独切换即可——按文件切换比全局开 jsdom 更精准，
 * 也不会让纯函数用例白白承担 DOM 初始化开销。
 *
 * 维护时应保持这个「默认 node、按需 jsdom」的分层，不要图省事把全局环境改成 jsdom。
 */

import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  // 需要 react 插件才能编译 .tsx 用例里的 JSX（自动运行时，无需在文件里 import React）。
  // 纯 .ts 用例不受影响：插件只处理带 JSX 的文件。
  plugins: [react()],
  test: {
    environment: 'node',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
})
