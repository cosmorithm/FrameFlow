import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const backend = { target: 'http://127.0.0.1:8080', changeOrigin: false }
export default defineConfig({
  plugins: [vue()],
  server: { proxy: { '/api': backend, '/media': backend, '/uploads': backend } },
})
