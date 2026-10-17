# /// script
# requires-python = ">=3.13,<3.14"
# dependencies = [
#     "normalizer-tr>=0.6,<1",
#     "numpy>=2.2",
#     "onnxruntime>=1.20",
# ]
# ///
"""Turkish text to speech with EMA Lightning (ONNX Runtime, CPU), with word and sentence timings.

    uv run --script ema_say.py --text-file in.txt --out-dir out/ [--speed 1.0] [--seed 0] [--model-dir DIR]

Writes out/speech.wav (mono 16-bit PCM, 48 kHz) and out/timings.json; prints one JSON summary line on stdout.
The weights are the ONNX export of canberkkkkkk/ema-lightning (ozcancelik/ema-lightning-onnx). EMA's text frontend
(frontend.py, chunker.py) and the export's host loop (plan() in web/tts.js) are reimplemented here, so neither torch
nor the ema-lightning package is needed.

Environment:
    EKIN_SPEECH_THREADS  ONNX Runtime intra-op threads (default 0 = all cores)
    EKIN_EMA_HF_REPO     Hugging Face repo with the ONNX graphs (default ozcancelik/ema-lightning-onnx)
    EKIN_EMA_REVISION    revision of that repo (default: the one this script was checked against)
"""

import argparse
import itertools
import json
import os
import re
import sys
import time
import unicodedata
import urllib.request
import wave
from pathlib import Path

ENGINE = "ema"
SENTENCE_PAUSE = 0.25
PARAGRAPH_PAUSE = 0.5
MAX_CHUNK_CHARS = 400  # written characters; fit() then keeps each spoken piece to about ten seconds
SEED = 0

_real_stdout = sys.stdout
sys.stdout = sys.stderr  # libraries must not write to stdout; only the final summary goes there

import numpy as np


class Failure(Exception):
    pass


# ---- text chunking (shared logic with okur_say.py and kokoro_say.py) -------------------------------------------------

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


def gaussian_noise(seed: int, count: int) -> np.ndarray:
    """gaussian() in the export's tts.js (mulberry32 + Box–Muller), vectorized; identical output."""
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


# ---- EMA frontend (ema_lightning frontend.py and chunker.py) ---------------------------------------------------------

POLICY = "fallback"  # notation the normalizer cannot resolve is read literally rather than kept as written
BLOCK_BYTES = 8 * 1024
TURKISH = frozenset("çğıöşüÇĞİÖŞÜ")
TYPOGRAPHY = str.maketrans({"’": "'", "‘": "'", "ʼ": "'", "´": "'", "`": "'", "“": '"', "”": '"', "„": '"',
                            "«": '"', "»": '"', "–": "-", "—": "-", "−": "-", "…": "..."})
UNSAFE = re.compile("[\x00-\x08\x0b-\x1f\x7f-\x9f؜‎‏‪-‮⁦-⁩]")
LETTERS_PER_SECOND = 18.0
MAX_SECONDS = 10.0
MAX_LETTERS = 250


class Frontend:
    """Written Turkish in, text in the model's alphabet out: normalizer-tr reads numbers, dates, units and
    abbreviations aloud, then Turkish lowercasing and the vocabulary drop whatever the model cannot read."""

    def __init__(self, vocab) -> None:
        from normalizer_tr import Normalizer

        self.vocab = frozenset(v for v in vocab if len(v) == 1)
        self.normalizer = Normalizer()

    def __call__(self, text: str) -> str:
        text = UNSAFE.sub(" ", text.encode("utf-8", "ignore").decode("utf-8"))
        if not text.strip():
            return ""
        return self.alphabet(" ".join(self.spoken(block) for block in blocks(text)))

    def spoken(self, text: str) -> str:
        try:
            return self.normalizer.normalize(text, ambiguity_policy=POLICY).normalized_text
        except Exception:  # noqa: BLE001 - invalid input or a resource limit: keep the words, not lose the sentence
            return text

    def alphabet(self, text: str) -> str:
        text = text.translate(TYPOGRAPHY).replace("İ", "i").replace("I", "ı").lower()
        out = []
        for ch in text:
            if ch not in TURKISH:
                ch = "".join(c for c in unicodedata.normalize("NFKD", ch) if not unicodedata.combining(c))
            out.append(ch if ch and all(c in self.vocab for c in ch) else " ")
        return re.sub(r"\s+", " ", "".join(out)).strip()


def blocks(text: str):
    """Split at whitespace into pieces the normalizer accepts in one call."""
    block, size = [], 0
    for word in text.split():
        n = len(word.encode("utf-8")) + 1
        if block and size + n > BLOCK_BYTES:
            yield " ".join(block)
            block, size = [], 0
        block.append(word)
        size += n
    if block:
        yield " ".join(block)


def finish(letters: str) -> str:
    """End every piece the way the model was trained: on a sentence end."""
    if letters.rstrip("\"')")[-1:] in (".", "!", "?"):
        return letters
    return letters.rstrip(",;:- ") + "."


def fit(tokens: list[str], frontend, speed: float) -> list[list[str]]:
    """Split a sentence's tokens until each spoken piece fits in about ten seconds, as EMA's chunker does: at the
    clause mark nearest the middle, else at the middle word."""
    limit = int(min(MAX_LETTERS, LETTERS_PER_SECOND * MAX_SECONDS * speed))
    if len(tokens) < 2 or len(frontend(" ".join(tokens))) <= limit:
        return [tokens]
    mid = len(tokens) / 2
    clause = [k for k in range(1, len(tokens)) if tokens[k - 1][-1:] in ",;:"]
    cut = min(clause or range(1, len(tokens)), key=lambda k: abs(k - mid))
    return fit(tokens[:cut], frontend, speed) + fit(tokens[cut:], frontend, speed)


# ---- EMA Lightning ---------------------------------------------------------------------------------------------------

HF_REPO = "ozcancelik/ema-lightning-onnx"
HF_REVISION = "13c431db0356b2f7fafb1247cd823ec0d777c820"
FILES = ("meta.json", "text.onnx", "sound.onnx", "decoder.onnx")
MAX_WORD_FRAMES = 250
MAX_FRAMES = 3000


def ensure_model(model_dir: Path) -> Path:
    """The directory holding meta.json and the three graphs, downloading them if missing."""
    if all((model_dir / f).is_file() for f in FILES):
        return model_dir
    repo = os.environ.get("EKIN_EMA_HF_REPO", HF_REPO)
    base = f"https://huggingface.co/{repo}/resolve/{os.environ.get('EKIN_EMA_REVISION', HF_REVISION)}"
    for name in (*FILES, "LICENSE", "NOTICE"):
        if not (model_dir / name).is_file():
            try:
                download(f"{base}/{name}", model_dir / name)
            except OSError:
                if name in FILES:
                    raise
    return model_dir


def word_index(letters: str) -> tuple[np.ndarray, np.ndarray]:
    """words() in tts.js: word of each letter (a word owns the spaces after it) and that word's first letter."""
    starts = [i for i, ch in enumerate(letters) if ch != " " and (i == 0 or letters[i - 1] == " ")] or [0]
    bounds = [0, *starts[1:], len(letters)]
    cw = np.zeros(len(letters), dtype=np.int64)
    wstart = np.zeros(len(letters), dtype=np.int64)
    for w, (a, b) in enumerate(itertools.pairwise(bounds)):
        cw[a:b], wstart[a:b] = w, a
    return cw, wstart


def plan(cw: np.ndarray, wstart: np.ndarray, d: np.ndarray):
    """plan() in tts.js: whole frames per word, the word of each frame, and the aligner's letter and frame positions
    measured in letters. d is frames per letter, already divided by the speed."""
    n_words = int(cw[-1]) + 1
    sums = np.zeros(n_words, dtype=np.float64)
    np.add.at(sums, cw, d.astype(np.float64))
    counts = np.clip(np.round(sums), 1, MAX_WORD_FRAMES).astype(np.int64)
    T = int(min(counts.sum(), MAX_FRAMES))
    fw = np.repeat(np.arange(n_words, dtype=np.int64), counts)[:T]
    first = np.cumsum(counts) - counts
    fp = ((np.arange(T) - first[fw]).astype(np.float64) / counts[fw]).astype(np.float32)
    c = np.maximum(d, np.float32(1e-4)).astype(np.float32)
    done = np.cumsum(c.astype(np.float64)).astype(np.float32)
    total = np.zeros(n_words, dtype=np.float32)
    np.add.at(total, cw, c)
    before = done - c
    wlen = np.bincount(cw, minlength=n_words).astype(np.float32)
    woff = np.concatenate([[0.0], np.cumsum(np.maximum(wlen[:-1], 1))]).astype(np.float32)
    cp = np.clip((done - before[wstart] - 0.5 * c) / np.maximum(total[cw], 1e-8), 0, 1)
    cg = (woff[cw] + cp * np.maximum(wlen[cw], 1)).astype(np.float32)
    fg = (woff[fw] + fp * np.maximum(wlen[fw], 1)).astype(np.float32)
    return counts, T, fw, cg, fg


class EMA:
    def __init__(self, model_dir: Path, threads: int) -> None:
        import onnxruntime as ort

        options = ort.SessionOptions()
        options.intra_op_num_threads = threads
        options.log_severity_level = 3
        self.meta = json.loads((model_dir / "meta.json").read_text())
        self.sessions = {name: ort.InferenceSession(str(model_dir / f"{name}.onnx"), options,
                                                    providers=["CPUExecutionProvider"])
                         for name in ("text", "sound", "decoder")}
        self.stoi = {ch: i for i, ch in enumerate(self.meta["vocab"])}
        self.letters = frozenset(v for v in self.meta["vocab"] if len(v) == 1 and v.isalpha())
        self.sample_rate = int(self.meta["sample_rate"])
        self.hop = int(self.meta["hop"])  # samples per latent frame: 1920 at 48 kHz = 40 ms

    def say(self, letters: str, speed: float, seed: int) -> tuple[np.ndarray, list[tuple[float, float] | None]]:
        """Audio for frontend output `letters`, and (start, end) seconds of each spoken word (None: no letters)."""
        ids = np.array([[self.stoi.get(ch, 1) for ch in letters]], dtype=np.int64)
        h, dur = (np.asarray(x) for x in self.sessions["text"].run(None, {"ids": ids}))
        d = (dur[0] / speed).astype(np.float32)
        cw, wstart = word_index(letters)
        counts, T, fw, cg, fg = plan(cw, wstart, d)
        steps, c = len(self.meta["times"]), int(self.meta["latent_dim"])
        noise = gaussian_noise(seed, steps * T * c).reshape(1, steps, T, c)
        (latents,) = self.sessions["sound"].run(None, {
            "h": h, "cg": cg[None], "fg": fg[None], "cw": cw[None], "fw": fw[None], "noise": noise})
        z = np.ascontiguousarray(np.asarray(latents, dtype=np.float32).transpose(0, 2, 1))
        (audio,) = self.sessions["decoder"].run(None, {"z": z})
        audio = np.asarray(audio, dtype=np.float32)[0]
        if len(audio) != T * self.hop:
            raise Failure(f"decoder returned {len(audio)} samples for {T} frames (hop {self.hop})")

        # Word w owns frames [ends[w-1], ends[w]); inside it, letters share them by predicted duration. A word's time
        # ends at its last letter, so the pause its punctuation and trailing space cause is excluded.
        frame_s = self.hop / self.sample_rate
        ends = np.cumsum(counts)
        spans: list[tuple[float, float] | None] = []
        for w in range(len(counts)):
            idx = np.nonzero(cw == w)[0]
            sounding = [i for i in idx if letters[i] in self.letters]
            f0 = float(ends[w] - counts[w])
            if not sounding or f0 >= T:
                spans.append(None)
                continue
            n = min(float(counts[w]), T - f0)
            total = float(d[idx].sum()) or 1.0
            before = float(d[idx[0]:sounding[0]].sum())
            through = float(d[idx[0]:sounding[-1] + 1].sum())
            spans.append(((f0 + n * before / total) * frame_s, (f0 + n * through / total) * frame_s))
        return audio, spans


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--text-file", required=True, type=Path)
    ap.add_argument("--out-dir", required=True, type=Path)
    ap.add_argument("--speed", type=float, default=1.0)
    ap.add_argument("--voice", default=None, help="ignored: EMA Lightning has one voice")
    ap.add_argument("--seed", type=int, default=SEED, help="noise seed: other values give other takes of the same text")
    ap.add_argument("--sentence-pause", type=float, default=SENTENCE_PAUSE, help="seconds between sentences")
    ap.add_argument("--paragraph-pause", type=float, default=PARAGRAPH_PAUSE, help="seconds between paragraphs")
    ap.add_argument("--lexicon", type=Path, default=None, help='JSON {"written": "spoken"} pronunciation dictionary')
    ap.add_argument("--model-dir", type=Path, default=Path("~/.cache/ekin/speech/ema"))
    args = ap.parse_args()
    t0 = time.perf_counter()
    for name, v in (("--sentence-pause", args.sentence_pause), ("--paragraph-pause", args.paragraph_pause)):
        if not 0.0 <= v <= 3.0:
            raise Failure(f"{name} must be between 0 and 3 seconds, got {v}")
    lexicon = load_lexicon(args.lexicon)
    if not 0.25 <= args.speed <= 4.0:
        raise Failure(f"--speed must be between 0.25 and 4, got {args.speed}")
    if args.seed < 0:
        raise Failure(f"--seed must not be negative, got {args.seed}")
    try:
        text = args.text_file.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as e:
        raise Failure(f"cannot read --text-file: {e}") from e
    pieces = chunks(text, "tr")
    if not pieces:
        raise Failure("the text file has no text to read")
    if args.voice:
        print(f"note: EMA Lightning has one voice, ignoring --voice {args.voice}", file=sys.stderr)

    tts = EMA(ensure_model(args.model_dir.expanduser()), int(os.environ.get("EKIN_SPEECH_THREADS", "0")))
    frontend = Frontend(tts.meta["vocab"])
    sr = tts.sample_rate
    render = lambda s: frontend(s).split()

    parts: list[np.ndarray] = []
    words: list[dict] = []
    segments: list[dict] = []
    offset = 0  # samples
    prev_paragraph = None
    k = 0  # piece number: every piece gets its own noise, as ema_lightning seeds them
    for paragraph, sentence in pieces:
        for tokens in fit(sentence.split(), frontend, args.speed):
            spoken_tokens = respell(tokens, lexicon)
            letters = frontend(" ".join(spoken_tokens))
            if not any(ch in tts.letters for ch in letters):
                continue  # nothing pronounceable
            if prev_paragraph is not None:
                gap = round((args.paragraph_pause if paragraph != prev_paragraph else args.sentence_pause) * sr)
                parts.append(np.zeros(gap, dtype=np.float32))
                offset += gap
            prev_paragraph = paragraph
            spoken = letters.split(" ")
            audio, spans = tts.say(finish(letters), args.speed, (args.seed * 1_000_003 + k) & 0xFFFFFFFF)
            k += 1
            spans = (spans + [None] * len(spoken))[:len(spoken)]  # finish() may drop a trailing silent word
            # The model opens with a short silence and closes with the pause its final punctuation asks for; trim
            # both so the inserted sentence and paragraph pauses set the rhythm.
            a, b = sounding_range(audio, sr)
            audio = audio[a:b]
            shift, limit = a / sr, len(audio) / sr
            spans = [None if s is None else (min(max(s[0] - shift, 0.0), limit), min(max(s[1] - shift, 0.0), limit))
                     for s in spans]
            spans = refine(audio, sr, spans)
            sec = offset / sr
            groups = map_tokens(spoken_tokens, spoken, render)
            for w in place_words(tokens, groups, spans):
                words.append({"text": w["text"], "start": w["start"] + sec, "end": w["end"] + sec})
            segments.append({"text": " ".join(tokens), "start": sec, "end": sec + len(audio) / sr})
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
        print(f"ema_say: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:  # noqa: BLE001
        import traceback

        traceback.print_exc(file=sys.stderr)
        print(f"ema_say: {type(e).__name__}: {e}", file=sys.stderr)
        sys.exit(1)
