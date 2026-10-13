# /// script
# requires-python = ">=3.13,<3.14"
# dependencies = [
#     "okur @ git+https://github.com/ahmethakanbesel/okur-tts@v1.0",
#     "numpy>=2.2",
#     "onnxruntime>=1.20",
# ]
#
# [tool.uv]
# # Only okur's text frontend is imported; the ONNX host loop is reimplemented below in numpy, so torch (a multi-GB
# # CUDA download on Linux) is never needed.
# exclude-dependencies = ["torch"]
# ///
"""Turkish text to speech with okur-tts (ONNX Runtime, CPU), with word and sentence timings.

    uv run --script okur_say.py --text-file in.txt --out-dir out/ [--speed 1.0] [--model-dir DIR]

Writes out/speech.wav (mono 16-bit PCM, 48 kHz) and out/timings.json; prints one JSON summary line on stdout.

Environment:
    KURGU_OKUR_SRC        path to an okur-tts checkout; its src/ is imported instead of the installed package
    KURGU_SPEECH_THREADS  ONNX Runtime intra-op threads (default 0 = all cores)
    KURGU_OKUR_HF_REPO    Hugging Face repo with the weights (default ahakan/okur-tts)
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

ENGINE = "okur"
SENTENCE_PAUSE = 0.25
PARAGRAPH_PAUSE = 0.5
MAX_CHUNK_CHARS = 400
SEED = 0

_real_stdout = sys.stdout
sys.stdout = sys.stderr  # libraries must not write to stdout; only the final summary goes there

if src := os.environ.get("KURGU_OKUR_SRC"):
    sys.path.insert(0, str(Path(src).expanduser() / "src"))

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


# ---- okur ------------------------------------------------------------------------------------------------------------

LETTERS = frozenset("abcdefghijklmnopqrstuvwxyzçğıöşüâîû")


def ensure_model(model_dir: Path) -> Path:
    """The directory holding the exported ONNX graphs and their config.json, downloading them if missing."""
    for d in (model_dir / "onnx", model_dir):
        cfg = d / "config.json"
        if cfg.is_file() and all((d / f).is_file() for f in json.loads(cfg.read_text())["files"].values()):
            return d
    repo = os.environ.get("KURGU_OKUR_HF_REPO", "ahakan/okur-tts")
    base = f"https://huggingface.co/{repo}/resolve/main"
    d = model_dir / "onnx"
    download(f"{base}/onnx/config.json", d / "config.json")
    for name in json.loads((d / "config.json").read_text())["files"].values():
        if not (d / name).is_file():
            download(f"{base}/onnx/{name}", d / name)
    if not (model_dir / "NOTICE").is_file():
        try:
            download(f"{base}/NOTICE", model_dir / "NOTICE")
        except OSError:
            pass
    return d


def gaussian_noise(seed: int, count: int) -> np.ndarray:
    """okur.export_web.gaussian_noise (mulberry32 + Box–Muller), vectorized; identical output."""
    n_uniform = count + (count & 1)
    i = np.arange(1, n_uniform + 1, dtype=np.uint64)
    z = ((np.uint64(seed & 0xFFFFFFFF) + i * np.uint64(0x6D2B79F5)) & np.uint64(0xFFFFFFFF)).astype(np.uint32)
    with np.errstate(over="ignore"):
        z = (z ^ (z >> np.uint32(15))) * (z | np.uint32(1))
        z ^= z + (z ^ (z >> np.uint32(7))) * (z | np.uint32(61))
    u = ((z ^ (z >> np.uint32(14))).astype(np.float64)) / 4294967296.0
    u1, u2 = 1.0 - u[0::2], u[1::2]
    r = np.sqrt(-2.0 * np.log(u1))
    out = np.empty(n_uniform, dtype=np.float32)
    out[0::2] = r * np.cos(2 * np.pi * u2)
    out[1::2] = r * np.sin(2 * np.pi * u2)
    return out[:count]


def word_index(letters: str) -> tuple[np.ndarray, np.ndarray, int]:
    """okur.model.timeline.words: word of each letter (a word owns the spaces after it) and that word's first letter."""
    starts = [i for i, ch in enumerate(letters) if ch != " " and (i == 0 or letters[i - 1] == " ")] or [0]
    bounds = [0, *starts[1:], len(letters)]
    cw = np.zeros(len(letters), dtype=np.int64)
    wstart = np.zeros(len(letters), dtype=np.int64)
    for w, (a, b) in enumerate(itertools.pairwise(bounds)):
        cw[a:b], wstart[a:b] = w, a
    return cw, wstart, len(bounds) - 1


def word_frames(dur: np.ndarray, cw: np.ndarray, n_words: int) -> np.ndarray:
    """okur.model.timeline.word_frames: whole frames per word, rounded on the running total."""
    per_word = np.zeros(n_words, dtype=np.float64)
    np.add.at(per_word, cw, dur.astype(np.float64))
    ends = np.round(np.cumsum(per_word)).astype(np.int64)
    return np.clip(np.diff(ends, prepend=0), 0, None)


def frame_timeline(counts: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    fw = np.repeat(np.arange(len(counts), dtype=np.int64), counts)
    start = (np.cumsum(counts) - counts)[fw]
    fp = ((np.arange(len(fw)) - start).astype(np.float64) / counts[fw].astype(np.float64)).astype(np.float32)
    return fw, fp


class Okur:
    def __init__(self, onnx_dir: Path, threads: int) -> None:
        import onnxruntime as ort
        from okur.frontend import Frontend, encode

        options = ort.SessionOptions()
        options.intra_op_num_threads = threads
        options.log_severity_level = 3
        self.config = json.loads((onnx_dir / "config.json").read_text())
        self.sessions = {k: ort.InferenceSession(str(onnx_dir / v), options, providers=["CPUExecutionProvider"])
                         for k, v in self.config["files"].items() if v.endswith(".onnx")}
        self.frontend, self.encode = Frontend(), encode
        self.sample_rate = int(self.config["sample_rate"])
        self.hop = int(self.config["hop"])  # samples per latent frame: 1920 at 48 kHz = 40 ms

    def say(self, letters: str, speed: float) -> tuple[np.ndarray, list[tuple[float, float] | None]]:
        """Audio for frontend output `letters`, and (start, end) seconds of each spoken word (None: no letters)."""
        ids = np.array([self.encode(letters)], dtype=np.int64)
        h, dur = (np.asarray(x) for x in self.sessions["text"].run(None, {"ids": ids}))
        dur = dur / speed
        cw, wstart, n_words = word_index(letters)
        counts = word_frames(dur[0].astype(np.float32), cw, n_words)
        fw, fp = frame_timeline(counts)
        steps, c = len(self.config["times"]), int(self.config["latent_dim"])
        noise = gaussian_noise(SEED, steps * len(fw) * c).reshape(steps, 1, len(fw), c)
        (z,) = self.sessions["sample"].run(None, {
            "h": h, "dur": dur.astype(np.float32), "cw": cw[None], "wstart": wstart[None], "fw": fw[None],
            "fp": fp[None].astype(np.float32), "noise": noise})
        (audio,) = self.sessions["decoder"].run(None, {"z": z})
        audio = np.asarray(audio, dtype=np.float32)[0]
        if len(audio) != len(fw) * self.hop:
            raise Failure(f"decoder returned {len(audio)} samples for {len(fw)} frames (hop {self.hop})")

        # Word w occupies frames [ends[w-1], ends[w]); inside it, letters share those frames by predicted duration.
        # A word's time ends at its last letter, so the pause its punctuation and trailing space cause is excluded.
        frame_s = self.hop / self.sample_rate
        ends = np.cumsum(counts)
        d = dur[0].astype(np.float64)
        spans: list[tuple[float, float] | None] = []
        for w in range(n_words):
            idx = np.nonzero(cw == w)[0]
            sounding = [i for i in idx if letters[i] in LETTERS]
            if not sounding or counts[w] == 0:
                spans.append(None)
                continue
            f0 = float(ends[w] - counts[w])
            total = float(d[idx].sum()) or 1.0
            before = float(d[idx[0]:sounding[0]].sum())
            through = float(d[idx[0]:sounding[-1] + 1].sum())
            spans.append(((f0 + counts[w] * before / total) * frame_s, (f0 + counts[w] * through / total) * frame_s))
        return audio, spans


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--text-file", required=True, type=Path)
    ap.add_argument("--out-dir", required=True, type=Path)
    ap.add_argument("--speed", type=float, default=1.0)
    ap.add_argument("--voice", default=None, help="ignored: okur v1.0 has one voice")
    ap.add_argument("--model-dir", type=Path, default=Path("~/.cache/kurgu/speech/okur"))
    args = ap.parse_args()
    t0 = time.perf_counter()
    if not 0.25 <= args.speed <= 4.0:
        raise Failure(f"--speed must be between 0.25 and 4, got {args.speed}")
    try:
        text = args.text_file.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as e:
        raise Failure(f"cannot read --text-file: {e}") from e
    pieces = chunks(text, "tr")
    if not pieces:
        raise Failure("the text file has no text to read")
    if args.voice:
        print(f"note: okur has one voice, ignoring --voice {args.voice}", file=sys.stderr)

    tts = Okur(ensure_model(args.model_dir.expanduser()), int(os.environ.get("KURGU_SPEECH_THREADS", "0")))
    sr = tts.sample_rate
    render = lambda s: tts.frontend(s).split()

    parts: list[np.ndarray] = []
    words: list[dict] = []
    segments: list[dict] = []
    offset = 0  # samples
    prev_paragraph = None
    for paragraph, sentence in pieces:
        letters = tts.frontend(sentence)
        if not any(ch in LETTERS for ch in letters):
            continue  # nothing pronounceable
        if prev_paragraph is not None:
            gap = round((PARAGRAPH_PAUSE if paragraph != prev_paragraph else SENTENCE_PAUSE) * sr)
            parts.append(np.zeros(gap, dtype=np.float32))
            offset += gap
        prev_paragraph = paragraph
        audio, spans = tts.say(letters, args.speed)
        # The model opens with a short silence and closes with the pause its final punctuation asks for; trim both
        # so the inserted sentence and paragraph pauses set the rhythm, like kokoro's trimming.
        a, b = sounding_range(audio, sr)
        audio = audio[a:b]
        shift, limit = a / sr, len(audio) / sr
        spans = [None if s is None else (min(max(s[0] - shift, 0.0), limit), min(max(s[1] - shift, 0.0), limit))
                 for s in spans]
        spans = refine(audio, sr, spans)
        sec = offset / sr
        tokens = sentence.split()
        groups = map_tokens(tokens, letters.split(" "), render)
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
        print(f"okur_say: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:  # noqa: BLE001
        import traceback

        traceback.print_exc(file=sys.stderr)
        print(f"okur_say: {type(e).__name__}: {e}", file=sys.stderr)
        sys.exit(1)
