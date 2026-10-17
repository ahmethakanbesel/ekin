# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "kokoro-onnx==0.6.1",
#     "numpy>=2.0",
#     "onnxruntime>=1.20",
# ]
# ///
"""English text to speech with Kokoro-82M (kokoro-onnx, ONNX Runtime, CPU), with word and sentence timings.

    uv run --script kokoro_say.py --text-file in.txt --out-dir out/ [--speed 1.0] [--voice af_heart] [--model-dir DIR]

Writes out/speech.wav (mono 16-bit PCM, 24 kHz) and out/timings.json; prints one JSON summary line on stdout.
Word times come from the per-phoneme durations the model itself predicts (the model-files-v1.1 exports carry a
`duration` output, read by kokoro-onnx >= 0.6 `create_timed`), so no forced alignment or guessing is involved.
Phonemization uses the espeak-ng library bundled in the espeakng-loader wheel; no system espeak-ng is needed.

Environment:
    EKIN_KOKORO_MODEL    model precision: fp32 (default, 326 MB), fp16 (164 MB) or int8 (114 MB, lower quality)
    EKIN_KOKORO_LANG     espeak language, en-us (default) or en-gb
    EKIN_SPEECH_THREADS  ONNX Runtime intra-op threads (default 0 = all cores)
"""

import argparse
import itertools
import json
import os
import re
import sys
import time
import urllib.request
import wave
from pathlib import Path

ENGINE = "kokoro"
SENTENCE_PAUSE = 0.25
PARAGRAPH_PAUSE = 0.5
CLAUSE_PAUSE = 0.1
MAX_CHUNK_CHARS = 300  # kokoro takes at most 510 phonemes per pass

_real_stdout = sys.stdout
sys.stdout = sys.stderr  # libraries must not write to stdout; only the final summary goes there

import numpy as np


class Failure(Exception):
    pass


# ---- text chunking (shared logic with kokoro_say.py) -----------------------------------------------------------------

ABBREVIATIONS = frozenset({
    "dr.", "prof.", "doç.", "av.", "sn.", "bkz.", "vb.", "vs.", "örn.", "yy.", "no.", "st.", "mr.", "mrs.", "ms.",
    "jr.", "sr.", "etc.", "e.g.", "i.e.", "fig.", "approx.", "dept.", "inc.", "ltd.", "co.",
})
_BOUNDARY = re.compile(r"[.!?…]+[\"'”’)\]]*\s+")


def split_sentences(paragraph: str, lang: str) -> list[str]:
    out: list[str] = []
    start = 0
    for m in _BOUNDARY.finditer(paragraph):
        nxt = paragraph[m.end():m.end() + 1]
        if not nxt or not (nxt.isupper() or nxt.isdigit() or nxt in "\"'“‘(«["):
            continue  # "3. katta": a period followed by a lowercase word is not a sentence end
        before = paragraph[start:m.end()].rstrip()
        last = before.split()[-1] if before.split() else ""
        if last.lower() in ABBREVIATIONS:
            continue
        if lang == "tr" and re.fullmatch(r"\d+\.", last):
            continue  # "1. Dünya Savaşı": an ordinal, not a sentence end
        out.append(paragraph[start:m.end()].strip())
        start = m.end()
    if paragraph[start:].strip():
        out.append(paragraph[start:].strip())
    return [piece for s in out for piece in split_long(s)]


def split_long(sentence: str) -> list[str]:
    """Break a run-on sentence at clause marks (then spaces) so no chunk exceeds MAX_CHUNK_CHARS."""
    if len(sentence) <= MAX_CHUNK_CHARS:
        return [sentence]
    mid = len(sentence) // 2
    cuts = [m.end() for m in re.finditer(r"[,;:]\s+", sentence)] or [m.end() for m in re.finditer(r"\s+", sentence)]
    if not cuts:
        return [sentence]
    cut = min(cuts, key=lambda c: abs(c - mid))
    return split_long(sentence[:cut].strip()) + split_long(sentence[cut:].strip())


def chunks(text: str, lang: str) -> list[tuple[int, str]]:
    """(paragraph index, sentence) pairs. Blank lines separate paragraphs; a single newline ends a sentence."""
    out: list[tuple[int, str]] = []
    paragraphs = [p for p in re.split(r"\n\s*\n", text.replace("\r\n", "\n")) if p.strip()]
    for pi, paragraph in enumerate(paragraphs):
        for line in paragraph.split("\n"):
            line = " ".join(line.split())
            if line:
                out += [(pi, s) for s in split_sentences(line, lang)]
    return out


def load_lexicon(path) -> dict[str, str]:
    """Pronunciation dictionary: {"written": "spoken"}; keys match case-insensitively."""
    if path is None:
        return {}
    try:
        raw = json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError) as e:
        raise Failure(f"cannot read --lexicon: {e}") from e
    if not isinstance(raw, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in raw.items()):
        raise Failure("--lexicon must be a JSON object of strings")
    return {k.strip().casefold(): v.strip() for k, v in raw.items() if k.strip() and v.strip()}


def respell(tokens: list[str], lexicon: dict[str, str]) -> list[str]:
    """Each written token as it should be spoken: the dictionary entry for its core (punctuation kept), else itself.
    One spoken entry per written token, so timings map back to the written words."""
    if not lexicon:
        return tokens
    out = []
    for t in tokens:
        i, j = 0, len(t)
        while i < j and not t[i].isalnum():
            i += 1
        while j > i and not t[j - 1].isalnum():
            j -= 1
        spoken = lookup(lexicon, t[i:j])
        out.append(t[:i] + spoken + t[j:] if spoken else t)
    return out


def lookup(lexicon: dict[str, str], core: str) -> str | None:
    """A token core's entry. A Turkish suffix after an apostrophe (PHQ-9'da) keeps its stem's entry."""
    spoken = lexicon.get(core.casefold())
    if spoken:
        return spoken
    k = min((core.find(q) for q in "'’" if q in core), default=-1)
    stem = lexicon.get(core[:k].casefold()) if k > 0 else None
    return stem + core[k:] if stem else None


def map_tokens(tokens: list[str], spoken: list[str], render) -> list[tuple[int, int, int, int]]:
    """Group written tokens with the spoken words they became: [(tok_a, tok_b, spk_a, spk_b)].

    A cut after token k is trusted when rendering the tokens before it reproduces the start of the spoken words, or
    rendering the tokens after it reproduces their end. Groups between trusted cuts hold one token unless the
    frontend's output depends on context across tokens.
    """
    n, m = len(tokens), len(spoken)
    cuts: dict[int, int] = {0: 0, n: m}
    conflict: set[int] = set()
    for k in range(1, n):
        found: set[int] = set()
        pre = render(" ".join(tokens[:k]))
        if pre == spoken[:len(pre)]:
            found.add(len(pre))
        suf = render(" ".join(tokens[k:]))
        if suf == spoken[m - len(suf):]:
            found.add(m - len(suf))
        if len(found) == 1:
            cuts[k] = found.pop()
        elif found:
            conflict.add(k)
    ordered: list[tuple[int, int]] = []
    for k in sorted(cuts):
        if k in conflict:
            continue
        if ordered and cuts[k] < ordered[-1][1]:
            continue  # not monotonic: drop the cut
        ordered.append((k, cuts[k]))
    if ordered[-1] != (n, m):
        while ordered and ordered[-1][1] > m:
            ordered.pop()
        ordered.append((n, m))
    return [(a, b, x, y) for (a, x), (b, y) in itertools.pairwise(ordered) if b > a]


QUIET_DB = -40.0  # below the chunk's loudest 10 ms frame by this much counts as silence
FRAME_S = 0.01


def loud_frames(audio: np.ndarray, sr: int) -> np.ndarray:
    hop = max(1, int(FRAME_S * sr))
    n = len(audio) // hop
    if n == 0:
        return np.zeros(0, dtype=bool)
    rms = np.sqrt((audio[:n * hop].astype(np.float64).reshape(n, hop) ** 2).mean(1))
    return rms > rms.max() * 10 ** (QUIET_DB / 20)


def sounding_range(audio: np.ndarray, sr: int, margin: float = 0.03) -> tuple[int, int]:
    """Sample range from the first to the last loud frame, with a small margin; the whole chunk if all quiet."""
    loud = np.nonzero(loud_frames(audio, sr))[0]
    if not len(loud):
        return 0, len(audio)
    hop = int(FRAME_S * sr)
    return max(0, int(loud[0] * hop - margin * sr)), min(len(audio), int((loud[-1] + 1) * hop + margin * sr))


def refine(audio: np.ndarray, sr: int, spans: list[tuple[float, float] | None]) -> list[tuple[float, float] | None]:
    """Snap word edges to the audio: a start inside silence moves to where sound begins, an end inside silence moves
    back to where it stops, and an end followed by sound that fades out before the next word moves to that fade.
    Model durations put pauses inside the neighbouring words; this keeps them out of the word spans."""
    loud = loud_frames(audio, sr)
    n = len(loud)
    if not n:
        return spans
    timed = [i for i, s in enumerate(spans) if s is not None]
    out = list(spans)
    min_len = 3  # frames
    for j, i in enumerate(timed):
        s, e = spans[i]
        a, b = min(n - 1, int(s / FRAME_S)), min(n, max(int(s / FRAME_S) + 1, round(e / FRAME_S)))
        nxt = min(n, int(spans[timed[j + 1]][0] / FRAME_S)) if j + 1 < len(timed) else n
        while a < b - min_len and not loud[a]:
            a += 1
        if b - 1 > a and not loud[b - 1]:
            while b > a + min_len and not loud[b - 1]:
                b -= 1
        else:  # still sounding at the end: follow the sound into a following pause, never past the next word
            k = b
            while k < nxt and loud[k]:
                k += 1
            if k < nxt:  # the sound did stop before the next word, so this tail belongs to this word
                b = k
        out[i] = (max(s, a * FRAME_S) if a * FRAME_S < e else s, b * FRAME_S if b * FRAME_S > a * FRAME_S else e)
    return out


def place_words(tokens: list[str], groups, spans: list[tuple[float, float] | None]) -> list[dict]:
    """Written tokens with times, from the (start, end) of each spoken word (None for silent ones)."""
    words: list[dict] = []
    for a, b, x, y in groups:
        timed = [s for s in spans[x:y] if s is not None]
        if not timed:
            continue  # the tokens produced no audible word (punctuation only)
        if b - a == y - x and all(s is not None for s in spans[x:y]):
            words += [{"text": tokens[a + i], "start": spans[x + i][0], "end": spans[x + i][1]} for i in range(b - a)]
            continue
        start, end = timed[0][0], timed[-1][1]
        if b - a == 1:
            words.append({"text": tokens[a], "start": start, "end": end})
            continue
        # Context-dependent group (rare): share its time among the tokens by length.
        weights = [max(len(t), 2) for t in tokens[a:b]]
        total, at = sum(weights), start
        for t, w in zip(tokens[a:b], weights, strict=True):
            nxt = at + (end - start) * w / total
            words.append({"text": t, "start": at, "end": nxt})
            at = nxt
    return words


def write_outputs(out_dir: Path, audio: np.ndarray, sample_rate: int, words: list[dict], segments: list[dict]) -> float:
    out_dir.mkdir(parents=True, exist_ok=True)
    pcm = (np.clip(audio, -1.0, 1.0) * 32767.0).round().astype("<i2")
    tmp = out_dir / "speech.wav.tmp"
    with wave.open(str(tmp), "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(sample_rate)
        w.writeframes(pcm.tobytes())
    tmp.replace(out_dir / "speech.wav")
    duration = round(len(pcm) / sample_rate, 3)
    r3 = lambda v: round(min(max(v, 0.0), duration), 3)
    doc = {
        "engine": ENGINE, "sampleRate": sample_rate, "duration": duration,
        "words": [{"text": w["text"], "start": r3(w["start"]), "end": r3(max(w["end"], w["start"]))} for w in words],
        "segments": [{"text": s["text"], "start": r3(s["start"]), "end": r3(s["end"])} for s in segments],
    }
    tmp = out_dir / "timings.json.tmp"
    tmp.write_text(json.dumps(doc, ensure_ascii=False))
    tmp.replace(out_dir / "timings.json")
    return duration


def download(url: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    tmp = dest.with_name(f"{dest.name}.{os.getpid()}.part")  # concurrent first runs must not clash
    print(f"downloading {url}", file=sys.stderr)
    with urllib.request.urlopen(url, timeout=60) as r, open(tmp, "wb") as f:
        while block := r.read(1 << 20):
            f.write(block)
    tmp.replace(dest)


# ---- kokoro ----------------------------------------------------------------------------------------------------------

RELEASE = "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.1"
MODELS = {"fp32": "kokoro-v1.0.onnx", "fp16": "kokoro-v1.0.fp16.onnx", "int8": "kokoro-v1.0.int8.onnx"}
VOICES = "voices-v1.0.bin"
PUNCT = frozenset(" ;:,.!?¡¿—…\"«»“”()-'")


def ensure_model(model_dir: Path, variant: str) -> tuple[Path, Path]:
    if variant not in MODELS:
        raise Failure(f"EKIN_KOKORO_MODEL must be one of {', '.join(MODELS)}, got {variant!r}")
    model, voices = model_dir / MODELS[variant], model_dir / VOICES
    for path in (model, voices):
        if not path.is_file():
            download(f"{RELEASE}/{path.name}", path)
    return model, voices


def spoken_words(timings) -> list[tuple[str, tuple[float, float] | None]]:
    """Group per-phoneme timings into space-separated words: (phonemes, (start, end) of its sounding phonemes)."""
    words: list[tuple[str, tuple[float, float] | None]] = []
    current: list = []
    for t in [*timings, None]:
        if t is None or t.phoneme == " ":
            if current:
                sounding = [p for p in current if p.phoneme not in PUNCT]
                span = (sounding[0].start, sounding[-1].end) if sounding else None
                words.append(("".join(p.phoneme for p in current), span))
            current = []
        else:
            current.append(t)
    return words


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--text-file", required=True, type=Path)
    ap.add_argument("--out-dir", required=True, type=Path)
    ap.add_argument("--speed", type=float, default=1.0)
    ap.add_argument("--voice", default="af_heart")
    ap.add_argument("--accent", choices=["en-us", "en-gb"], default=None, help="overrides EKIN_KOKORO_LANG")
    ap.add_argument("--quality", choices=["fp32", "fp16", "int8"], default=None, help="overrides EKIN_KOKORO_MODEL")
    ap.add_argument("--sentence-pause", type=float, default=SENTENCE_PAUSE, help="seconds between sentences")
    ap.add_argument("--paragraph-pause", type=float, default=PARAGRAPH_PAUSE, help="seconds between paragraphs")
    ap.add_argument("--lexicon", type=Path, default=None, help='JSON {"written": "spoken"} pronunciation dictionary')
    ap.add_argument("--model-dir", type=Path, default=Path("~/.cache/ekin/speech/kokoro"))
    args = ap.parse_args()
    t0 = time.perf_counter()
    for name, v in (("--sentence-pause", args.sentence_pause), ("--paragraph-pause", args.paragraph_pause)):
        if not 0.0 <= v <= 3.0:
            raise Failure(f"{name} must be between 0 and 3 seconds, got {v}")
    lexicon = load_lexicon(args.lexicon)
    if not 0.5 <= args.speed <= 2.0:
        raise Failure(f"--speed must be between 0.5 and 2 for kokoro, got {args.speed}")
    try:
        text = args.text_file.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as e:
        raise Failure(f"cannot read --text-file: {e}") from e
    pieces = chunks(text, "en")
    if not pieces:
        raise Failure("the text file has no text to read")
    lang = args.accent or os.environ.get("EKIN_KOKORO_LANG", "en-us")

    model_path, voices_path = ensure_model(args.model_dir.expanduser(), args.quality or os.environ.get("EKIN_KOKORO_MODEL", "fp32"))
    import onnxruntime as ort
    from kokoro_onnx import Kokoro

    options = ort.SessionOptions()
    options.intra_op_num_threads = int(os.environ.get("EKIN_SPEECH_THREADS", "0"))
    options.log_severity_level = 3
    session = ort.InferenceSession(str(model_path), options, providers=["CPUExecutionProvider"])
    kokoro = Kokoro.from_session(session, str(voices_path))
    if not kokoro.has_timings:
        raise Failure(f"{model_path.name} has no duration output; use the model-files-v1.1 exports")
    if args.voice not in kokoro.voices:
        raise Failure(f"unknown voice {args.voice!r}; available: {', '.join(kokoro.get_voices())}")

    def render(s: str) -> list[str]:
        return kokoro.tokenizer.phonemize(s, lang).split() if s.strip() else []

    parts: list[np.ndarray] = []
    words: list[dict] = []
    segments: list[dict] = []
    sr = 24000
    offset = 0
    prev_paragraph = None
    for paragraph, sentence in pieces:
        tokens = sentence.split()
        spoken_tokens = respell(tokens, lexicon)
        spoken_sentence = " ".join(spoken_tokens)
        if not render(spoken_sentence):
            continue  # nothing pronounceable
        audio, sr, timings = kokoro.create_timed(spoken_sentence, args.voice, speed=args.speed, lang=lang, trim=True,
                                                 sentence_pause=0.0, clause_pause=CLAUSE_PAUSE)
        audio = np.asarray(audio, dtype=np.float32)
        spoken = spoken_words(timings)
        if not len(audio) or all(span is None for _, span in spoken):
            continue  # punctuation only ("— ... —"): no word to say
        if prev_paragraph is not None:
            gap = round((args.paragraph_pause if paragraph != prev_paragraph else args.sentence_pause) * sr)
            parts.append(np.zeros(gap, dtype=np.float32))
            offset += gap
        prev_paragraph = paragraph
        sec = offset / sr
        groups = map_tokens(spoken_tokens, [w for w, _ in spoken], render)
        # The model lengthens a word's last phoneme to make the pause after it; snap word edges to the audio.
        spans = refine(audio, sr, [span for _, span in spoken])
        for w in place_words(tokens, groups, spans):
            words.append({"text": w["text"], "start": w["start"] + sec, "end": w["end"] + sec})
        segments.append({"text": sentence, "start": sec, "end": sec + len(audio) / sr})
        parts.append(audio)
        offset += len(audio)
    if not parts:
        raise Failure("the text has nothing pronounceable")

    duration = write_outputs(args.out_dir, np.concatenate(parts), sr, words, segments)
    print(json.dumps({"duration": duration, "words": len(words), "seconds": round(time.perf_counter() - t0, 3)}),
          file=_real_stdout, flush=True)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Failure as e:
        print(f"kokoro_say: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:  # noqa: BLE001
        import traceback

        traceback.print_exc(file=sys.stderr)
        print(f"kokoro_say: {type(e).__name__}: {e}", file=sys.stderr)
        sys.exit(1)
