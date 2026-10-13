# Speech helpers

Two self-contained [PEP 723](https://peps.python.org/pep-0723/) scripts that turn narration text into a WAV plus
word and sentence timings. The Go worker shells out to them.

| Script | Language | Engine | Output sample rate |
|---|---|---|---|
| `okur_say.py` | Turkish | [okur-tts](https://github.com/ahmethakanbesel/okur-tts) v1.0, ONNX Runtime CPU | 48000 Hz |
| `kokoro_say.py` | English | Kokoro-82M via [kokoro-onnx](https://github.com/thewh1teagle/kokoro-onnx) 0.6.1, ONNX Runtime CPU | 24000 Hz |

## Usage

```sh
uv run --script tools/speech/okur_say.py   --text-file in.txt --out-dir out/ [--speed 1.0] [--model-dir DIR]
uv run --script tools/speech/kokoro_say.py --text-file in.txt --out-dir out/ [--speed 1.0] [--voice af_heart] [--model-dir DIR]
```

- `--text-file`: UTF-8 text. A blank line separates paragraphs; a single newline also ends a sentence.
- `--out-dir`: created if missing. Gets `speech.wav` (mono 16-bit PCM at the model's native rate) and `timings.json`.
- `--speed`: okur accepts 0.25–4, kokoro 0.5–2.
- `--voice`: kokoro only (default `af_heart`; an unknown name fails and lists the voices). okur has one voice and
  ignores the flag, with a note on stderr.
- `--model-dir`: where the weights are kept. Defaults: `~/.cache/kurgu/speech/okur` and `~/.cache/kurgu/speech/kokoro`.
  Missing files are downloaded on the first run.

Stdout holds exactly one line, written at the end: `{"duration": 20.82, "words": 34, "seconds": 1.41}` (seconds is the
wall time inside Python). Progress, downloads and warnings go to stderr. On failure the exit code is non-zero and
stderr ends with `okur_say: <reason>` or `kokoro_say: <reason>`.

`timings.json`:

```json
{"engine": "okur", "sampleRate": 48000, "duration": 20.82,
 "words": [{"text": "Toplantı", "start": 0.03, "end": 0.58}, {"text": "3.", "start": 0.66, "end": 1.04}],
 "segments": [{"text": "Toplantı 3. katta, saat 09:30'da.", "start": 0.0, "end": 2.94}]}
```

- `words[].text` is always the input token as written: whitespace-separated, punctuation kept (`"3."`, `"katta,"`,
  `"09:30'da."`, `"$1,250.50"`). Spoken forms (`üçüncü`, `dokuz otuzda`) are never output. A token read as several
  words (`2026` → `iki bin yirmi altı`) gets one entry spanning all of them. Tokens that produce no sound (`—`) are
  left out. Emoji are read aloud by both engines ("gülümseyen yüz", "slightly smiling face"); strip them first if
  that is unwanted.
- `segments` hold one entry per synthesized chunk, with the chunk's text as given: a sentence, or part of one when a
  sentence runs past 400 characters (okur) or 300 characters (kokoro). Those are split at a comma or semicolon near
  the middle, or at a space if there is none.
- Times are seconds rounded to 3 decimals, monotonic and inside `duration`. There are 0.25 s of silence between
  sentences and 0.5 s between paragraphs. Each chunk's own leading and trailing silence is trimmed first.

## Prerequisites

Only `uv` (and `git`, for the okur dependency). Neither script needs torch or a system espeak-ng:

- okur imports only the okur package's text frontend (`okur.frontend.Frontend`, the same one `OnnxSynthesizer` uses).
  The ONNX host loop (word timeline, seeded noise) is reimplemented in numpy. It produces bit-identical audio to
  `okur.export_web.OnnxSynthesizer` (checked). `exclude-dependencies = ["torch"]` keeps the multi-GB CUDA torch off
  the server.
- kokoro-onnx phonemizes with the espeak-ng library bundled in the `espeakng-loader` wheel. `brew install espeak-ng`
  is not needed.

## Environment variables

| Variable | Script | Meaning |
|---|---|---|
| `KURGU_OKUR_SRC` | okur | Path to an okur-tts checkout. Its `src/` is put first on `sys.path`, so local frontend changes apply without reinstalling. The pinned git package (`okur @ git+https://github.com/ahmethakanbesel/okur-tts@v1.0`) still supplies the dependencies. |
| `KURGU_OKUR_HF_REPO` | okur | Hugging Face repo for the weights (default `ahakan/okur-tts`). |
| `KURGU_KOKORO_MODEL` | kokoro | `fp32` (default, 326 MB), `fp16` (164 MB) or `int8` (114 MB). int8 is about 1.5× faster but sounds worse (spectral correlation 0.92 against fp32). |
| `KURGU_KOKORO_LANG` | kokoro | `en-us` (default) or `en-gb` espeak accent. |
| `KURGU_SPEECH_THREADS` | both | ONNX Runtime intra-op threads. The default 0 uses all cores. |

To skip the okur download, pre-populate the cache with `uv run hf download ahakan/okur-tts --local-dir
~/.cache/kurgu/speech/okur`. Only `onnx/` is used.

## How the timings are made

- **okur**: the text graph predicts frames per letter (1 frame = hop 1920 samples / 48 kHz = 40 ms). The word
  timeline the model is conditioned on (`words` + `word_frames`) fixes how many frames each word gets, and the
  audio is exactly `frames × 1920` samples (asserted). A word's time runs from its first to its last letter. The
  pause its trailing space or punctuation causes is excluded.
- **kokoro**: the model-files-v1.1 ONNX exports have a `duration` output. kokoro-onnx 0.6 `create_timed` turns it
  into per-phoneme times, and phonemes are grouped into words at spaces.
- **Both**: each word's edges are snapped to the audio. A start inside silence moves to where the sound begins, and
  an end inside silence moves back to where it stops. Kokoro in particular lengthens a word's last phoneme to make
  the pause after it, so its raw ends run 100–300 ms late before a pause.
- **Written-token mapping**: each spoken word is mapped back to an input token by re-running the frontend (okur's
  normalizer, or espeak for kokoro) on every prefix and suffix of the sentence. A cut between tokens is trusted when
  either side reproduces the full rendering. In the rare case where the rendering depends on context across tokens,
  the group's time is shared among its tokens by length.

## Accuracy

Measured against `ffmpeg silencedetect` (-40 dBFS, ≥ 60 ms) on the test paragraphs:

- **okur**: word edges at real pauses are within a median of 20 ms of the silence boundaries (e.g. `09:30'da.` ends
  15 ms after the sound stops, `Kaygı` starts 4 ms before it begins, `Bu` 1 ms). The only silence inside a word was
  the closure of the double k in `hakkında`, which is correct.
- **kokoro**: median 30 ms. Ends are within 10–35 ms of the pause, and starts are 10–60 ms after the sound begins.
  Silences reported inside words were stop closures (`expected`, `effects`).
- ffprobe duration matches `timings.duration` within 1 ms.

That is well within what karaoke-style captions need. Expect word boundaries inside continuous speech (no pause) to
be within about one frame of the model (40 ms for okur, 25 ms for kokoro).

## Speed (Apple M4, CPU, 10 cores)

| | Audio | Wall time | Real-time factor |
|---|---|---|---|
| okur (21 s Turkish) | 20.8 s | 0.64 s idle machine, 1.4 s under load (+0.5–0.8 s `uv run` startup) | 0.03–0.07 (15–33× faster than real time) |
| kokoro fp32 (61 English words) | 26.3 s | 6.3 s idle, 11–14 s under load | 0.24–0.5 |
| kokoro int8 | 26.4 s | 7.6–8.5 s under load | ~0.3 |

"Under load" means Chrome was using about 4 cores. kokoro spends about 2.8 s of its time loading and optimizing the
326 MB graph on every run, okur well under 0.5 s.

First run, which downloads everything (wheels go into uv's cache, weights into `--model-dir`):

- okur: about 30 MB of wheels plus a git build of okur, and 22 MB of ONNX weights. 17 s in total here.
- kokoro: about 35 MB of wheels, a 326 MB model and 28 MB of voices. 22 s in total here (fp16 and int8 are fetched
  only when selected).
