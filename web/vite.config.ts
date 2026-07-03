/**
 * 业务说明：本文件是前端构建配置，负责把 React、Tailwind 和开发代理装配成可本地调试、可生产打包的应用。
 * 它影响资料库、阅读器、设置页等所有前端页面的加载方式，并通过 chunk 拆分控制首屏资源和白屏风险。
 * 维护时应重点关注 vendor 分包、React 运行时依赖顺序、后端 API 代理和生产构建产物是否仍能被 Go 服务静态托管。
 */

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [tailwindcss(), react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('/node_modules/')) {
            return undefined
          }
          // 仅把自包含的 React 运行时核心单独成 chunk：它们彼此依赖但不依赖任何其他三方库，
          // 因此其他 vendor 代码可以单向依赖它而不会形成跨 chunk 循环。
          // 任何调用 React.forwardRef/createContext 的库（react-virtuoso、@yui540/comimi-react 等）
          // 都必须留在 vendor，与 React 核心保持单向引用，
          // 否则会复现 "Cannot read properties of undefined (reading 'forwardRef')" 白屏。
          if (/\/node_modules\/(react|react-dom|scheduler)\//.test(id)) {
            return 'react-core'
          }
          return 'vendor'
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      }
    }
  }
})
