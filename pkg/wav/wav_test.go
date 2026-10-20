package wav_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/pkg/wav"
)

// file builds a WAV whose frame i holds sample(i) in every channel,
// with an extra chunk before the data like some writers add.
func file(code uint16, channels, rate, bits, frames int, sample func(i int) float64, extensible bool) []byte {
	block := channels * bits / 8
	fmtBody := binary.LittleEndian.AppendUint16(nil, code)
	if extensible {
		fmtBody = binary.LittleEndian.AppendUint16(nil, 0xFFFE)
	}
	fmtBody = binary.LittleEndian.AppendUint16(fmtBody, uint16(channels))
	fmtBody = binary.LittleEndian.AppendUint32(fmtBody, uint32(rate))
	fmtBody = binary.LittleEndian.AppendUint32(fmtBody, uint32(rate*block))
	fmtBody = binary.LittleEndian.AppendUint16(fmtBody, uint16(block))
	fmtBody = binary.LittleEndian.AppendUint16(fmtBody, uint16(bits))
	if extensible {
		fmtBody = binary.LittleEndian.AppendUint16(fmtBody, 22)
		fmtBody = binary.LittleEndian.AppendUint16(fmtBody, uint16(bits))
		fmtBody = binary.LittleEndian.AppendUint32(fmtBody, 0)
		fmtBody = binary.LittleEndian.AppendUint16(fmtBody, code)
		fmtBody = append(fmtBody, make([]byte, 14)...)
	}
	var data []byte
	for i := range frames {
		for range channels {
			switch {
			case code == 1 && bits == 16:
				data = binary.LittleEndian.AppendUint16(data, uint16(int16(sample(i))))
			case code == 3 && bits == 32:
				data = binary.LittleEndian.AppendUint32(data, math.Float32bits(float32(sample(i))))
			}
		}
	}
	var out []byte
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(4+8+len(fmtBody)+8+3+1+8+len(data)))
	out = append(out, "WAVEfmt "...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(fmtBody)))
	out = append(out, fmtBody...)
	out = append(out, "LIST"...)
	out = binary.LittleEndian.AppendUint32(out, 3)
	out = append(out, 'a', 'b', 'c', 0) // odd size, padded
	out = append(out, "data"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
	return append(out, data...)
}

func samples16(t *testing.T, b []byte) []int16 {
	t.Helper()
	i := bytes.Index(b, []byte("data"))
	if i < 0 {
		t.Fatal("no data chunk")
	}
	n := int(binary.LittleEndian.Uint32(b[i+4:]))
	var out []int16
	for j := i + 8; j < i+8+n; j += 2 {
		out = append(out, int16(binary.LittleEndian.Uint16(b[j:])))
	}
	return out
}

func TestCutCopiesTheSpanAndFadesItsEnds(t *testing.T) {
	// 1000 frames/s, frame i holds the value i (1-based so the fade shows).
	src := file(1, 1, 1000, 16, 3000, func(i int) float64 { return float64(i + 1) }, false)
	var out bytes.Buffer
	length, err := wav.Cut(bytes.NewReader(src), &out, 500*time.Millisecond, 1500*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if length != time.Second {
		t.Fatalf("length = %s", length)
	}
	got := samples16(t, out.Bytes())
	if len(got) != 1000 {
		t.Fatalf("frames = %d, want 1000", len(got))
	}
	if got[0] != 0 || got[5] != int16(math.Round(506*0.5)) || got[10] != 511 || got[500] != 1001 || got[999] != 0 {
		t.Fatalf("samples = %v … %v … %v", got[:12], got[500], got[990:])
	}
	if binary.LittleEndian.Uint32(out.Bytes()[4:]) != uint32(out.Len()-8) {
		t.Fatalf("RIFF size %d for a file of %d bytes", binary.LittleEndian.Uint32(out.Bytes()[4:]), out.Len())
	}

	// The cut is itself a WAV that cuts again.
	var again bytes.Buffer
	if _, err := wav.Cut(bytes.NewReader(out.Bytes()), &again, 0, 100*time.Millisecond, 0); err != nil {
		t.Fatal(err)
	}
	if n := len(samples16(t, again.Bytes())); n != 100 {
		t.Fatalf("second cut = %d frames", n)
	}
}

func TestCutFormats(t *testing.T) {
	tests := []struct {
		name string
		src  []byte
	}{
		{"stereo 16-bit", file(1, 2, 24000, 16, 48000, func(int) float64 { return 1000 }, false)},
		{"float 32-bit", file(3, 1, 24000, 32, 48000, func(int) float64 { return 0.5 }, false)},
		{"extensible 16-bit", file(1, 1, 24000, 16, 48000, func(int) float64 { return 1000 }, true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			length, err := wav.Cut(bytes.NewReader(tt.src), &out, 250*time.Millisecond, 750*time.Millisecond, 5*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if length != 500*time.Millisecond {
				t.Fatalf("length = %s", length)
			}
			if !bytes.HasPrefix(out.Bytes(), []byte("RIFF")) || !bytes.Contains(out.Bytes()[:60], []byte("WAVEfmt ")) {
				t.Fatalf("header = %q", out.Bytes()[:16])
			}
		})
	}
}

func TestCutRejects(t *testing.T) {
	good := file(1, 1, 1000, 16, 1000, func(int) float64 { return 1 }, false)
	tests := []struct {
		name       string
		src        []byte
		start, end time.Duration
	}{
		{"not a wav", []byte("ID3 an mp3 header ........"), 0, time.Second},
		{"span after the audio", good, 2 * time.Second, 3 * time.Second},
		{"empty span", good, 500 * time.Millisecond, 500 * time.Millisecond},
		{"not pcm", file(2, 1, 1000, 16, 10, func(int) float64 { return 0 }, false), 0, time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := wav.Cut(bytes.NewReader(tt.src), &bytes.Buffer{}, tt.start, tt.end, 0); err == nil {
				t.Fatal("cut succeeded")
			}
		})
	}
}

func TestCutClampsToTheAudio(t *testing.T) {
	src := file(1, 1, 1000, 16, 1000, func(int) float64 { return 1 }, false)
	length, err := wav.Cut(bytes.NewReader(src), &bytes.Buffer{}, 900*time.Millisecond, 5*time.Second, 0)
	if err != nil || length != 100*time.Millisecond {
		t.Fatalf("length = %s, %v", length, err)
	}
}
