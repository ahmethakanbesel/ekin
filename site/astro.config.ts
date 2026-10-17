import { defineConfig } from "astro/config"

// SITE_URL is the public origin, used for canonical and social links.
export default defineConfig({
  site: process.env.SITE_URL || "http://localhost:4321",
  output: "static",
  build: { inlineStylesheets: "auto" },
})
