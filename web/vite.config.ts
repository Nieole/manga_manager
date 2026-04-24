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
          if (id.includes('/node_modules/react/') || id.includes('/node_modules/react-dom/') || id.includes('/node_modules/react-router/') || id.includes('/node_modules/react-router-dom/')) {
            return 'framework'
          }
          if (id.includes('/node_modules/lucide-react/')) {
            return 'icons'
          }
          if (id.includes('/node_modules/date-fns/')) {
            return 'date'
          }
          if (id.includes('/node_modules/axios/')) {
            return 'http'
          }
          if (id.includes('/node_modules/react-select/') || id.includes('/node_modules/react-virtuoso/')) {
            return 'ui-vendor'
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
