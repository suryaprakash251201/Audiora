import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

// The dev server proxies the API to a locally running Go server so the
// browser sees a single origin and CORS never enters the picture in
// development.
const API_TARGET = process.env.AUDIORA_API ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    // Bind on all interfaces so a phone on the same network can load the dev
    // build for testing the Capacitor shell.
    host: true,
    proxy: {
      '/api': {
        target: API_TARGET,
        changeOrigin: true,
        // The WebSocket route needs this or the sync socket never upgrades.
        ws: true,
      },
      '/healthz': { target: API_TARGET, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    rollupOptions: {
      output: {
        // Split the vendor bundle so a change to app code does not
        // invalidate React in everyone's browser cache.
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
  },
})
