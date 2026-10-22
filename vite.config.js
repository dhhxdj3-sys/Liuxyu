import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'

const API_TARGET = 'http://127.0.0.1:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
  
  ],
  server: {
    host: '0.0.0.0', // 允许局域网访问
    proxy: {
      '/api': {
        target: API_TARGET,
        changeOrigin: true
      }
    }
  },
  preview: {
    proxy: {
      '/api': {
        target: API_TARGET,
        changeOrigin: true
      }
    }
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
})
