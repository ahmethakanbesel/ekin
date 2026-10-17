import { writeFileSync } from "node:fs"
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin } from "vite"

const backend = process.env.EKIN_BACKEND_URL ?? "http://127.0.0.1:8080"
const outDir = path.resolve(__dirname, "dist")

// The Go binary embeds web/dist (web/embed.go, `//go:embed all:dist`), so
// dist/.gitkeep is tracked to keep the directory — and the embed — valid
// before the first build. Vite empties outDir on every build; this puts
// the placeholder back once the bundle is written.
function keepGitkeep(): Plugin {
  return {
    name: "ekin:keep-gitkeep",
    apply: "build",
    closeBundle() {
      writeFileSync(path.join(outDir, ".gitkeep"), "")
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss(), keepGitkeep()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    outDir,
    emptyOutDir: true,
    // One admin bundle, embedded in the binary and served locally; the
    // default 500 kB warning is aimed at public sites.
    chunkSizeWarningLimit: 1200,
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": { target: backend, changeOrigin: true },
      "/healthz": { target: backend, changeOrigin: true },
    },
  },
})
