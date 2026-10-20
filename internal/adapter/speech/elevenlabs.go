package speech

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/ahmethakanbesel/ekin/internal/adapter/elevenlabs"
)

// maxChunkRunes bounds one ElevenLabs request. Every current model takes
// at least 5,000 characters; shorter requests keep a failed retry cheap.
const maxChunkRunes = 2500

func (s *Synth) sayElevenLabs(ctx context.Context, lang, textFile, outDir string, o Options) (Timings, error) {
	key := ""
	if s.ElevenLabsKey != nil {
		key = s.ElevenLabsKey(ctx)
	}
	if key == "" {
		return Timings{}, errors.New("speech: ElevenLabs needs an API key (System → Credentials)")
	}
	raw, err := os.ReadFile(textFile) //nolint:gosec // the caller's narration file
	if err != nil {
		return Timings{}, fmt.Errorf("speech: %w", err)
	}
	chunks := narrationChunks(string(raw))
	if len(chunks) == 0 {
		return Timings{}, errors.New("speech: the text has nothing to read")
	}
	el := o.ElevenLabs
	if el == nil {
		el = &ElevenLabsOptions{Model: "eleven_multilingual_v2", Stability: 0.5, SimilarityBoost: 0.75, SpeakerBoost: true}
	}
	speed := o.Speed
	if speed == 0 {
		speed = 1
	}
	sentencePause, paragraphPause := o.SentencePause, o.ParagraphPause
	if sentencePause == 0 {
		sentencePause = 0.25
	}
	if paragraphPause == 0 {
		paragraphPause = 0.5
	}
	client := &elevenlabs.Client{APIKey: key, BaseURL: s.ElevenLabsURL}
	spokenTokens := make([][]string, len(chunks))
	spoken := make([]string, len(chunks))
	for i, c := range chunks {
		spokenTokens[i] = respell(c.tokens, o.Lexicon)
		spoken[i] = strings.Join(spokenTokens[i], " ")
	}

	const rate = elevenlabs.SampleRate
	var (
		pcm      []int16
		timings  = Timings{Engine: EngineElevenLabs, Voice: o.Voice, Model: el.Model, SampleRate: rate}
		previous = -1
	)
	for i, c := range chunks {
		req := elevenlabs.SpeakRequest{
			Text: spoken[i], Model: el.Model, Language: lang, Seed: o.Seed,
			Settings: elevenlabs.VoiceSettings{Stability: el.Stability, SimilarityBoost: el.SimilarityBoost, Style: el.Style,
				UseSpeakerBoost: el.SpeakerBoost, Speed: speed},
		}
		if i > 0 {
			req.PreviousText = spoken[i-1]
		}
		if i+1 < len(spoken) {
			req.NextText = spoken[i+1]
		}
		sp, err := client.Speak(ctx, o.Voice, req)
		if err != nil {
			return Timings{}, fmt.Errorf("speech: %w", err)
		}
		a, b := soundingRange(sp.PCM, rate)
		if a >= b {
			continue
		}
		if previous >= 0 {
			gap := sentencePause
			if c.paragraph != previous {
				gap = paragraphPause
			}
			pcm = append(pcm, make([]int16, int(math.Round(gap*rate)))...)
		}
		previous = c.paragraph
		offset := float64(len(pcm)) / rate
		shift, length := float64(a)/rate, float64(b-a)/rate
		for _, w := range wordTimes(c.tokens, spokenTokens[i], sp) {
			w.Start = offset + clamp(w.Start-shift, 0, length)
			w.End = offset + clamp(w.End-shift, 0, length)
			timings.Words = append(timings.Words, w)
		}
		timings.Segments = append(timings.Segments, Segment{Text: strings.Join(c.tokens, " "), Start: offset, End: offset + length})
		pcm = append(pcm, sp.PCM[a:b]...)
	}
	if len(timings.Words) == 0 {
		return Timings{}, errors.New("speech: ElevenLabs returned no speech")
	}
	timings.Duration = round3(float64(len(pcm)) / rate)
	for i := range timings.Words {
		w := &timings.Words[i]
		w.Start, w.End = round3(w.Start), round3(math.Max(w.End, w.Start))
	}
	for i := range timings.Segments {
		seg := &timings.Segments[i]
		seg.Start, seg.End = round3(seg.Start), round3(seg.End)
	}
	if err := writeOutputs(outDir, pcm, rate, timings); err != nil {
		return Timings{}, err
	}
	return timings, nil
}

// chunk is a run of narration read in one request.
type chunk struct {
	paragraph int
	tokens    []string
}

var sentenceEnd = regexp.MustCompile(`[.!?…]["'”’)\]]*$`)

var paragraphBreak = regexp.MustCompile(`\n\s*\n`)

// narrationChunks splits narration into one chunk per paragraph (blank
// lines separate them), and a paragraph longer than maxChunkRunes at the
// sentence end nearest the limit, else at a word.
func narrationChunks(text string) []chunk {
	var out []chunk
	paragraphs := paragraphBreak.Split(strings.ReplaceAll(text, "\r\n", "\n"), -1)
	pi := 0
	for _, p := range paragraphs {
		tokens := strings.Fields(p)
		if len(tokens) == 0 {
			continue
		}
		for len(tokens) > 0 {
			n, size, cut := 0, 0, 0
			for n < len(tokens) && (n == 0 || size+1+len([]rune(tokens[n])) <= maxChunkRunes) {
				size += len([]rune(tokens[n])) + 1
				n++
				if sentenceEnd.MatchString(tokens[n-1]) {
					cut = n
				}
			}
			if n == len(tokens) || cut == 0 {
				cut = n
			}
			out = append(out, chunk{paragraph: pi, tokens: tokens[:cut]})
			tokens = tokens[cut:]
		}
		pi++
	}
	return out
}

// respell replaces each token's core (punctuation kept) with its
// dictionary entry, one spoken token per written token, as the scripts do.
func respell(tokens []string, lexicon map[string]string) []string {
	if len(lexicon) == 0 {
		return tokens
	}
	lex := make(map[string]string, len(lexicon))
	for k, v := range lexicon {
		if k, v = strings.TrimSpace(k), strings.TrimSpace(v); k != "" && v != "" {
			lex[strings.ToLower(k)] = v
		}
	}
	out := make([]string, len(tokens))
	for i, t := range tokens {
		r := []rune(t)
		a, b := 0, len(r)
		for a < b && !isWordRune(r[a]) {
			a++
		}
		for b > a && !isWordRune(r[b-1]) {
			b--
		}
		if spoken, ok := lookup(lex, string(r[a:b])); ok {
			out[i] = string(r[:a]) + spoken + string(r[b:])
		} else {
			out[i] = t
		}
	}
	return out
}

// lookup finds a token core's entry. A Turkish suffix after an
// apostrophe (PHQ-9'da) keeps its stem's entry: "pe ha ku dokuz'da".
func lookup(lex map[string]string, core string) (string, bool) {
	if spoken, ok := lex[strings.ToLower(core)]; ok {
		return spoken, true
	}
	if k := strings.IndexAny(core, "'’"); k > 0 {
		if spoken, ok := lex[strings.ToLower(core[:k])]; ok {
			return spoken + core[k:], true
		}
	}
	return "", false
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// wordTimes gives each written token the time of the characters its
// spoken form (which may be several words) was read with: from the first
// letter or digit to the last, or the whole form when it has none. Tokens
// with no sound are left out. The request text is spoken joined by spaces.
func wordTimes(written, spoken []string, sp elevenlabs.Speech) []Word {
	text := []rune(strings.Join(spoken, " "))
	// The alignment covers the request text character by character; if a
	// provider ever changes that, map positions proportionally.
	index := func(i int) int {
		if len(sp.Characters) == len(text) {
			return i
		}
		return min(len(sp.Characters)-1, i*len(sp.Characters)/max(1, len(text)))
	}
	var out []Word
	pos := 0
	for wi, tok := range spoken {
		r := []rune(tok)
		start, end, found := math.Inf(1), math.Inf(-1), false
		for pass := 0; pass < 2 && !found; pass++ {
			for k, ch := range r {
				if pass == 0 && !isWordRune(ch) {
					continue
				}
				j := index(pos + k)
				if j < 0 || j >= len(sp.Characters) {
					continue
				}
				start, end, found = math.Min(start, sp.Starts[j]), math.Max(end, sp.Ends[j]), true
			}
		}
		pos += len(r) + 1
		if !found || end <= start || wi >= len(written) {
			continue
		}
		out = append(out, Word{Text: written[wi], Start: start, End: end})
	}
	return out
}

// soundingRange is the sample range from the first to the last 10 ms
// frame within 40 dB of the loudest, with 30 ms to spare, as the scripts
// trim each chunk.
func soundingRange(pcm []int16, rate int) (int, int) {
	hop := rate / 100
	n := len(pcm) / hop
	if n == 0 {
		return 0, len(pcm)
	}
	rms := make([]float64, n)
	peak := 0.0
	for f := range n {
		sum := 0.0
		for _, v := range pcm[f*hop : (f+1)*hop] {
			sum += float64(v) * float64(v)
		}
		rms[f] = math.Sqrt(sum / float64(hop))
		peak = math.Max(peak, rms[f])
	}
	if peak == 0 {
		return 0, 0
	}
	threshold := peak * math.Pow(10, -40.0/20)
	first, last := -1, -1
	for f, v := range rms {
		if v > threshold {
			if first < 0 {
				first = f
			}
			last = f
		}
	}
	margin := rate * 3 / 100
	return max(0, first*hop-margin), min(len(pcm), (last+1)*hop+margin)
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// writeOutputs writes speech.wav (mono 16-bit PCM) and timings.json.
func writeOutputs(outDir string, pcm []int16, rate int, t Timings) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	wav := make([]byte, 44+2*len(pcm))
	copy(wav[0:], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(36+2*len(pcm))) //nolint:gosec // bounded by the narration length
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)              // PCM
	binary.LittleEndian.PutUint16(wav[22:], 1)              // mono
	binary.LittleEndian.PutUint32(wav[24:], uint32(rate))   //nolint:gosec // a sample rate
	binary.LittleEndian.PutUint32(wav[28:], uint32(rate*2)) //nolint:gosec // a byte rate
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(2*len(pcm))) //nolint:gosec // bounded by the narration length
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(wav[44+2*i:], uint16(v)) //nolint:gosec // two's complement bytes
	}
	if err := writeAtomic(filepath.Join(outDir, "speech.wav"), wav); err != nil {
		return err
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(outDir, "timings.json"), raw)
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
