// usage: node render.mjs [--half] [--workers N] [--out name.mp4]
import { renderVideo } from "@revideo/renderer";

const arg = (k, d) => { const i = process.argv.indexOf(`--${k}`); return i > 0 ? process.argv[i + 1] : d; };
const half = process.argv.includes("--half");
const t0 = Date.now();
const file = await renderVideo({
  projectFile: half ? "./src/project-half.ts" : "./src/project.ts",
  settings: {
    outFile: arg("out", half ? "revideo-half.mp4" : "revideo.mp4"),
    outDir: "../out",
    workers: Number(arg("workers", 1)),
    logProgress: false,
    // assets/ and shared/ live one level up.
    viteConfig: { server: { fs: { allow: [".."] } }, logLevel: "error" },
    // Revideo always adds --single-process; desktop Chrome on macOS dies with it, the headless shell does not.
    puppeteer: process.env.CHROME ? { executablePath: process.env.CHROME, args: ["--no-sandbox"] } : { headless: "shell" },
  },
});
console.log(JSON.stringify({ out: file, wall_s: (Date.now() - t0) / 1000 }));
