import { useEffect, useState } from "react";
import { AbsoluteFill, Img, continueRender, delayRender, staticFile, useCurrentFrame, useVideoConfig } from "remotion";
import spec from "../../shared/spec.json";

const clamp = (x: number, a = 0, b = 1) => Math.min(b, Math.max(a, x));
const easeOutCubic = (x: number) => 1 - Math.pow(1 - x, 3);

const pages: (typeof spec.words)[] = [];
for (let i = 0; i < spec.words.length; i += spec.wordsPerPage) pages.push(spec.words.slice(i, i + spec.wordsPerPage));

const useInter = () => {
  const [handle] = useState(() => delayRender("Inter"));
  useEffect(() => {
    const face = new FontFace("Inter", `url(${staticFile("Inter.ttf")}) format("truetype")`, { weight: "100 900" });
    face.load().then(() => { document.fonts.add(face); continueRender(handle); });
  }, [handle]);
};

export const Bench = () => {
  useInter();
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const t = frame / fps;
  const p = t / spec.duration;
  const kt = easeOutCubic(clamp(t / 0.8));
  let pi = -1;
  pages.forEach((pg, i) => { if (t >= pg[0].start) pi = i; });

  return (
    <AbsoluteFill style={{
      fontFamily: "Inter, sans-serif", color: "#fff", overflow: "hidden",
      background: `linear-gradient(180deg, hsl(${230 + 40 * p}, 55%, 16%), hsl(${290 + 50 * p}, 60%, 30%))`,
    }}>
      <div style={{ position: "absolute", left: 90, top: 200, width: 900, fontWeight: 800, fontSize: 92, lineHeight: 1.05,
        letterSpacing: -2, opacity: kt, transform: `translateY(${(1 - kt) * 60}px)` }}>{spec.title}</div>

      <div style={{ position: "absolute", left: 90, top: 400, width: 900, height: 640, borderRadius: 36, overflow: "hidden",
        opacity: easeOutCubic(clamp((t - 0.3) / 0.8)) }}>
        <Img src={staticFile("photo.png")} style={{ position: "absolute", left: 0, top: -17.5, width: 900, height: 675,
          transformOrigin: "50% 50%", transform: `translate(${-40 * p}px, ${-20 * p}px) scale(${1 + 0.25 * p})` }} />
      </div>

      {spec.bullets.map((b, i) => {
        const k = easeOutCubic(clamp((t - b.at) / 0.6));
        return (
          <div key={i} style={{ position: "absolute", left: 90, top: 1090 + i * 140, width: 900, height: 116, borderRadius: 28,
            background: "rgba(255,255,255,0.12)", display: "flex", alignItems: "center", fontWeight: 600, fontSize: 46,
            boxSizing: "border-box", paddingLeft: 40, opacity: k, transform: `translateX(${(1 - k) * -120}px)` }}>
            <i style={{ width: 22, height: 22, borderRadius: 11, background: "#ffd166", marginRight: 28, flex: "none" }} />
            <span>{b.text}</span>
          </div>
        );
      })}

      <div style={{ position: "absolute", left: 60, top: 1540, width: 960, height: 240, display: "flex", flexWrap: "wrap",
        justifyContent: "center", alignContent: "center", fontWeight: 700, fontSize: 58, lineHeight: 1.25 }}>
        {pi >= 0 && pages[pi].map((w, j) => {
          const current = t >= w.start && t < w.end;
          return (
            <span key={j} style={{ margin: "0 10px", display: "inline-block", color: current ? "#ffd166" : "#fff",
              opacity: t < w.start ? 0.45 : 1, transform: current ? "scale(1.08)" : "none" }}>{w.text}</span>
          );
        })}
      </div>

      <div style={{ position: "absolute", left: 0, top: 1890, width: 1080, height: 14, background: "rgba(255,255,255,0.15)" }}>
        <div style={{ position: "absolute", left: 0, top: 0, height: 14, width: 1080 * clamp(p), background: "#ffd166" }} />
      </div>
    </AbsoluteFill>
  );
};
