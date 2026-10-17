# Composition kit

`composition.html` is an HTML page whose every frame is a pure function of time.
The renderer loads it from a local server, calls `window.kurgu.renderAt(t)` for
each frame, and screenshots the page. Load the kit with:

```html
<!doctype html>
<meta charset="utf-8">
<link rel="stylesheet" href="/kit/kurgu.css">
<script src="/kit/kurgu.js"></script>
```

`/kit/kurgu.css` bundles these fonts, all with full Turkish glyphs, resets the
page, disables CSS animations and transitions, and styles captions. Use only them:

| Family | Weights | Character |
|---|---|---|
| Inter | 400, 600, 800 | neutral sans, the default |
| Lora | 400, 700 | warm serif for editorial and reflective styles |
| Nunito | 400, 800 | rounded, friendly |
| Oswald | 600 | condensed, punchy headlines |
| Caveat | 600 | handwriting, for annotations |
| JetBrains Mono | 500 | monospace, for numbers and code-like labels |
| Fraunces | 400–700 (variable) | soft, rounded display serif; friendly headlines |
| Instrument Serif | 400, 400 italic | fine, high-contrast editorial serif for large titles; too light for small text |

## Declaring the composition

```js
let captions = () => {};
Kurgu.composition({
  width: {{.Width}}, height: {{.Height}}, fps: 30,
  // Narration length plus a short tail, read from the timings file.
  duration: async () => (await Kurgu.timings()).duration + 1.5,
  async setup() {                      // runs once before the first frame
    const t = await Kurgu.timings();   // narration/timings.json
    captions = Kurgu.captions(document.getElementById("captions"), t.words, { maxWords: 4 });
  },
  render(t) {                          // may be async (e.g. to seek videos)
    captions(t);
    // set styles/text from t only
  },
});
```

## Helpers (`window.Kurgu`)

| Helper | Use |
|---|---|
| `progress(t, start, end)` | 0..1 clamped |
| `tween(t, start, duration, ease?)` | eased 0..1, default `ease.inOutCubic` |
| `lerp(a, b, p)`, `clamp(x, lo, hi)` | arithmetic |
| `ease.linear / inQuad / outQuad / inOutQuad / outCubic / inOutCubic / outExpo / outBack / outElastic` | easing |
| `random(seed)` | deterministic PRNG; never use `Math.random()` |
| `timings(path?)` | narration timings: `{duration, words: [{text, start, end}], segments: [{text, start, end}]}` |
| `captions(el, words, {maxWords, holdSeconds})` | karaoke captions; returns `update(t)`; current word gets class `on` |
| `video(el)` | wraps a muted `<video>`; `await v.seek(t)` shows the frame at t (loops) |
| `scenes([{start, end, el}])` | returns `update(t)` that shows the active scene and gives local progress |

## Rules the check enforces

- No network: every image, video and font is a local file (stock via `kurgu tool stock`{{if .Media.Library}}
  or `kurgu tool library use`{{end}}).
- No `requestAnimationFrame`, `setInterval`, `setTimeout` loops, `Date.now()`,
  `performance.now()`, CSS animations or transitions, autoplaying media.
- `duration` covers the narration plus at most 4 s.
- The page must not throw, log errors, or request missing files.

## Music (optional)

`music.js` defines a Tone.js score rendered offline and mixed under the voice
(ducked automatically):

```js
window.kurguMusic = (Tone, { duration }) => {
  const pad = new Tone.PolySynth(Tone.Synth, { oscillator: { type: "sine" } }).toDestination();
  pad.volume.value = -14;
  const chords = [["C3", "E3", "G3"], ["A2", "C3", "E3"], ["F2", "A2", "C3"], ["G2", "B2", "D3"]];
  for (let i = 0; i * 4 < duration; i++) pad.triggerAttackRelease(chords[i % 4], 3.8, i * 4);
};
```

Schedule notes at absolute times (seconds) or on `Tone.getTransport()`. Do not
call `Tone.start()`. Keep it calm and quiet: the voice is the point. Run
`kurgu tool music` to render `music.wav` and check its level.
