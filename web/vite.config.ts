import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('/node_modules/')) {
            return undefined
          }
          // 仅把自包含的 React 运行时核心单独成 chunk：它们彼此依赖但不依赖任何其他三方库，
          // 因此其他 vendor 代码可以单向依赖它而不会形成跨 chunk 循环。
          // 任何调用 React.forwardRef/createContext 的库（react-select/@emotion、react-virtuoso、
          // @yui540/comimi-react 等）都必须留在 vendor，与 React 核心保持单向引用，
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
