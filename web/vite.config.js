import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src')
    }
  },
  css: {
    preprocessorOptions: {
      scss: {
        api: 'modern-compiler'
      }
    }
  },
  server: {
    host: true,
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8888',
        changeOrigin: true,
        ws: true, // 启用 WebSocket 代理
        // 配置SSE支持
        configure: (proxy, options) => {
          proxy.on('proxyReq', (proxyReq, req, res) => {
            // SSE请求需要禁用缓冲
            if (req.url.includes('/worker/logs/stream')) {
              proxyReq.setHeader('Cache-Control', 'no-cache')
              proxyReq.setHeader('Connection', 'keep-alive')
            }
          })
        }
      },
      '/static': {
        target: 'http://localhost:8888',
        changeOrigin: true
      }
    }
  }
})
