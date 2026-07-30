import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('@xyflow') || id.includes('dagre')) {
              return 'xyflow';
            }
            if (id.includes('react-window')) {
              return 'windowing';
            }
            if (id.includes('react') || id.includes('zustand')) {
              return 'vendor';
            }
          }
        },
      },
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/ws': {
        target: 'ws://localhost:8942',
        ws: true,
      },
      '/api': {
        target: 'http://localhost:8942',
        changeOrigin: true,
      },
    },
  },
});
