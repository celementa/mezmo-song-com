import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import { readFileSync } from 'node:fs'
import path from 'node:path'

const root = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  plugins: [
    react(),
    {
      // Бэкенда у mezmo-song.com нет: снимок настроек лендинга вшивается в
      // index.html, иначе SPA не знает дизайна и цены и падает в classic.
      // `<` экранируется, чтобы текст настроек не закрыл тег script.
      name: 'inline-settings',
      transformIndexHtml(html: string) {
        const settings = readFileSync(path.join(root, 'intl/settings.json'), 'utf8')
        const json = JSON.stringify(JSON.parse(settings)).replace(/</g, '\\u003c')
        return html.replace('<!--__SETTINGS__-->', `<script>window.__SETTINGS__=${json}</script>`)
      },
    },
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
      '@': path.resolve(root, './src'),
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
