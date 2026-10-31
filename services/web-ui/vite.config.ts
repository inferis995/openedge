import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

const VENDOR_CHUNKS: Record<string, string[]> = {
    vendor: ['react', 'react-dom', 'scheduler'],
    charts: ['echarts', 'zrender', 'recharts'],
    ui: ['@radix-ui/react-dialog', '@radix-ui/react-select', '@radix-ui/react-tabs'],
}

// https://vite.dev/config/
export default defineConfig({
    plugins: [react()],
    resolve: {
        alias: {
            "@": path.resolve(__dirname, "./src"),
        },
    },
    server: {
        host: '0.0.0.0', // Ensure binding to all interfaces for external access
        port: 3000,
        proxy: {
            '/api': {
                target: 'http://localhost:8081',
                changeOrigin: true,
            },
        },
    },
    build: {
        target: 'es2015',
        chunkSizeWarningLimit: 1500,
        rollupOptions: {
            output: {
                // Libraries that change rarely, in chunks of their own so a new
                // release of the app does not make browsers download them again.
                // Rolldown (Vite 8) takes this as a function only.
                manualChunks(id: string) {
                    if (!id.includes('node_modules')) return undefined
                    for (const [chunk, pkgs] of Object.entries(VENDOR_CHUNKS)) {
                        if (pkgs.some(p => id.includes(`/node_modules/${p}/`))) return chunk
                    }
                    return undefined
                },
            },
        },
    }
})
