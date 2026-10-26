import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        configure(proxy) {
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('origin', 'http://127.0.0.1:8080')
          })
        },
      },
    },
  },
})
