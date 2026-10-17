// Playwright comparison driver (same loop as vanilla-go): renderAt(t) -> JPEG screenshot -> ffmpeg.
// usage (from bench/render): node vanilla-pw/render.mjs [--scale 0.5] [--out out/pw.mp4] [--chrome PATH] [--fast]
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, join } from "node:path";
import { chromium } from "playwright-core";

const arg = (k, d) => { const i = process.argv.indexOf(`--${k}`); return i > 0 ? process.argv[i + 1] : d; };
const SCALE = Number(arg("scale", 1));
const OUT = arg("out", "out/pw.mp4");
const CHROME = arg("chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome");
const types = { ".html": "text/html", ".png": "image/png", ".ttf": "font/ttf" };

const server = createServer(async (req, res) => {
  try { const b = await readFile(join(process.cwd(), decodeURIComponent(req.url.split("?")[0]))); res.writeHead(200, { "content-type": types[extname(req.url)] ?? "application/octet-stream" }); res.end(b); }
  catch { res.writeHead(404); res.end(); }
}).listen(0, "127.0.0.1");
await new Promise(r => server.once("listening", r));

const t0 = Date.now();
const args = ["--font-render-hinting=none", "--force-color-profile=srgb", "--disable-lcd-text", "--disable-gpu",
  "--run-all-compositor-stages-before-draw"];
if (process.argv.includes("--fast")) args.push("--disable-frame-rate-limit", "--disable-gpu-vsync");
const browser = await chromium.launch({ executablePath: CHROME, headless: true, args });
const page = await browser.newPage({ viewport: { width: 1080, height: 1920 }, deviceScaleFactor: SCALE });
await page.goto(`http://127.0.0.1:${server.address().port}/vanilla/composition.html`);
const { duration, fps } = await page.evaluate(async () => { await ekin.ready; return { duration: ekin.duration, fps: ekin.fps }; });

const ff = spawn("ffmpeg", ["-y", "-loglevel", "error", "-f", "image2pipe", "-framerate", String(fps), "-c:v", "mjpeg", "-i", "-",
  "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p", "-movflags", "+faststart", OUT], { stdio: ["pipe", "inherit", "inherit"] });
const frames = Math.round(duration * fps);
for (let f = 0; f < frames; f++) {
  await page.evaluate(t => ekin.renderAt(t), f / fps);
  const buf = await page.screenshot({ type: "jpeg", quality: 90 });
  if (!ff.stdin.write(buf)) await new Promise(r => ff.stdin.once("drain", r));
}
ff.stdin.end();
await new Promise(r => ff.on("close", r));
await browser.close();
server.close();
console.log(JSON.stringify({ out: OUT, frames, wall_s: (Date.now() - t0) / 1000 }));
