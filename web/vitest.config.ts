/**
 * 业务说明：本文件是前端单元测试配置，负责用 Vitest 运行 src 下的纯逻辑测试（i18n 归一化、
 * 阅读进度计算等），保障这些不依赖 DOM 的业务函数在改动后仍符合契约。
 * 维护时应保持 node 测试环境与最小依赖；需要组件级测试时再引入 jsdom 与 testing-library。
 */

import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    // 首批测试全部为纯函数（无 DOM），用 node 环境避免引入 jsdom 依赖与其带来的不稳定性。
    environment: 'node',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
})
