/** @jsxImportSource @revideo/2d/lib */
import { Circle, Gradient, Img, Layout, Rect, Txt, makeScene2D } from "@revideo/2d";
import { createSignal, linear } from "@revideo/core";
import spec from "../../../shared/spec.json";
import photo from "../../../assets/photo.png";

const clamp = (x: number, a = 0, b = 1) => Math.min(b, Math.max(a, x));
const easeOutCubic = (x: number) => 1 - Math.pow(1 - x, 3);
// HTML top-left px -> Revideo centre-origin coordinates.
const X = (left: number, w: number) => left + w / 2 - spec.width / 2;
const Y = (top: number, h: number) => top + h / 2 - spec.height / 2;

const pages: (typeof spec.words)[] = [];
for (let i = 0; i < spec.words.length; i += spec.wordsPerPage) pages.push(spec.words.slice(i, i + spec.wordsPerPage));

export default makeScene2D("bench", function* (view) {
  yield document.fonts.load('800 92px "Inter"');
  yield document.fonts.ready;

  // Same pure-function-of-t model as the vanilla composition: one linear time signal drives everything.
  const time = createSignal(0);
  const p = () => time() / spec.duration;
  const page = () => { let pi = -1; pages.forEach((pg, i) => { if (time() >= pg[0].start) pi = i; }); return pi; };
  const word = (j: number) => (page() < 0 ? undefined : pages[page()][j]);

  view.add(
    <>
      <Rect width={spec.width} height={spec.height}
        fill={() => new Gradient({ type: "linear", from: [0, -960], to: [0, 960], stops: [
          { offset: 0, color: `hsl(${230 + 40 * p()}, 55%, 16%)` },
          { offset: 1, color: `hsl(${290 + 50 * p()}, 60%, 30%)` },
        ] })} />

      <Txt text={spec.title} fontFamily="Inter" fontWeight={800} fontSize={92} letterSpacing={-2} fill="#fff"
        offset={[-1, -1]} x={90 - 540} y={() => 200 - 960 + 5 + (1 - easeOutCubic(clamp(time() / 0.8))) * 60}
        opacity={() => easeOutCubic(clamp(time() / 0.8))} />

      <Rect x={X(90, 900)} y={Y(400, 640)} width={900} height={640} radius={36} clip
        opacity={() => easeOutCubic(clamp((time() - 0.3) / 0.8))}>
        <Img src={photo} width={900} height={675} x={() => -40 * p()} y={() => -20 * p()} scale={() => 1 + 0.25 * p()} />
      </Rect>

      {spec.bullets.map((b, i) => {
        const k = () => easeOutCubic(clamp((time() - b.at) / 0.6));
        return (
          <Rect layout alignItems="center" paddingLeft={40} width={900} height={116} radius={28} fill="rgba(255,255,255,0.12)"
            x={() => X(90, 900) + (1 - k()) * -120} y={Y(1090 + i * 140, 116)} opacity={k}>
            <Circle size={22} fill="#ffd166" marginRight={28} />
            <Txt text={b.text} fontFamily="Inter" fontWeight={600} fontSize={46} fill="#fff" />
          </Rect>
        );
      })}

      <Layout layout direction="row" wrap="wrap" justifyContent="center" alignContent="center"
        x={X(60, 960)} y={Y(1540, 240)} width={960} height={240}>
        {Array.from({ length: spec.wordsPerPage }, (_, j) => {
          const current = () => { const w = word(j); return !!w && time() >= w.start && time() < w.end; };
          return (
            <Txt text={() => word(j)?.text ?? ""} fontFamily="Inter" fontWeight={700} fontSize={58} lineHeight={58 * 1.25}
              marginLeft={10} marginRight={10} fill={() => (current() ? "#ffd166" : "#fff")}
              opacity={() => { const w = word(j); return !w ? 0 : time() < w.start ? 0.45 : 1; }}
              scale={() => (current() ? 1.08 : 1)} />
          );
        })}
      </Layout>

      <Rect x={0} y={Y(1890, 14)} width={1080} height={14} fill="rgba(255,255,255,0.15)" />
      <Rect offset={[-1, 0]} x={-540} y={Y(1890, 14)} width={() => 1080 * clamp(p())} height={14} fill="#ffd166" />
    </>,
  );

  yield* time(spec.duration, spec.duration, linear);
});
