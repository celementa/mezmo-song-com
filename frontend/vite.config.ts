import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

export default defineConfig({
  plugins: [
    react(),
    {
      // Снимаем render-blocking с CSS-бандла: hero в #root на инлайн-стилях,
      // поэтому первый paint не должен ждать загрузки 57 КБ CSS.
      name: 'async-css',
      transformIndexHtml: {
        order: 'post',
        handler(html: string) {
          return html.replace(
            /<link rel="stylesheet"[^>]*href="(\/assets\/index-[^"]+\.css)"[^>]*>/,
            '<link rel="preload" as="style" href="$1" onload="this.onload=null;this.rel=\'stylesheet\'">' +
              '<noscript><link rel="stylesheet" href="$1"></noscript>',
          )
        },
      },
    },
  ],
  resolve: {
    alias: {
      '@': path.resolve(path.dirname(fileURLToPath(import.meta.url)), './src'),
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-react': ['react', 'react-dom', 'react-router-dom'],
          'vendor-ui': ['lucide-react', '@radix-ui/react-slot', 'class-variance-authority', 'clsx', 'tailwind-merge'],
          'vendor-form': ['react-hook-form', '@hookform/resolvers', 'zod'],
        },
      },
    },
    chunkSizeWarningLimit: 400,
  },
  server: {
    host: '0.0.0.0',
    port: Number(process.env.PORT) || 5173,
    strictPort: true,
    allowedHosts: true,
    headers: {
      'Cache-Control': 'no-store',
    },
    proxy: {
      '/api': {
        target: `http://127.0.0.1:${process.env.BACKEND_PORT || 8080}`,
        changeOrigin: true,
      },
      '/uploads': {
        target: `http://127.0.0.1:${process.env.BACKEND_PORT || 8080}`,
        changeOrigin: true,
      },
    },
  },
})
