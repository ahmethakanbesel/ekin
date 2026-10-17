// Package wav cuts spans out of PCM WAV files without decoding them:
// the span's frames are copied as they are, with short fades at both
// ends so a cut never clicks.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// Format codes in a WAV fmt chunk.
const (
	formatPCM        = 1
	formatFloat      = 3
	formatExtensible = 0xFFFE
)

// Format is what a WAV file's fmt chunk says about its samples.
type Format struct {
	Code          uint16 // formatPCM or formatFloat (an extensible file's subformat)
	Channels      int
	SampleRate    int
	BitsPerSample int
	BlockAlign    int // bytes per frame
}

// header is a parsed WAV file: its format, the fmt chunk as stored and
// where its samples are.
type header struct {
	format     Format
	fmtChunk   []byte
	dataOffset int64
	dataSize   int64
}

func readHeader(r io.ReadSeeker) (header, error) {
	var h header
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return h, fmt.Errorf("read wav header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return h, errors.New("not a WAV file")
	}
	pos := int64(12)
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return h, errors.New("WAV file has no data chunk")
		}
		id, size := string(ch[0:4]), int64(binary.LittleEndian.Uint32(ch[4:8]))
		pos += 8
		switch id {
		case "fmt ":
			if size < 16 || size > 1024 {
				return h, errors.New("WAV fmt chunk has a bad size")
			}
			h.fmtChunk = make([]byte, size)
			if _, err := io.ReadFull(r, h.fmtChunk); err != nil {
				return h, fmt.Errorf("read wav fmt chunk: %w", err)
			}
			f, err := parseFormat(h.fmtChunk)
			if err != nil {
				return h, err
			}
			h.format = f
		case "data":
			if h.fmtChunk == nil {
				return h, errors.New("WAV data comes before its fmt chunk")
			}
			h.dataOffset, h.dataSize = pos, size
			end, err := r.Seek(0, io.SeekEnd)
			if err != nil {
				return h, err
			}
			// A writer that streamed the file may leave the size open.
			if h.dataSize == 0xFFFFFFFF || h.dataOffset+h.dataSize > end {
				h.dataSize = end - h.dataOffset
			}
			h.dataSize -= h.dataSize % int64(h.format.BlockAlign)
			return h, nil
		}
		if id != "fmt " {
			if _, err := r.Seek(size, io.SeekCurrent); err != nil {
				return h, err
			}
		}
		pos += size
		if size%2 == 1 { // chunks are padded to an even size
			if _, err := r.Seek(1, io.SeekCurrent); err != nil {
				return h, err
			}
			pos++
		}
	}
}

func parseFormat(b []byte) (Format, error) {
	f := Format{
		Code:          binary.LittleEndian.Uint16(b[0:2]),
		Channels:      int(binary.LittleEndian.Uint16(b[2:4])),
		SampleRate:    int(binary.LittleEndian.Uint32(b[4:8])),
		BlockAlign:    int(binary.LittleEndian.Uint16(b[12:14])),
		BitsPerSample: int(binary.LittleEndian.Uint16(b[14:16])),
	}
	if f.Code == formatExtensible {
		if len(b) < 26 {
			return f, errors.New("WAV extensible fmt chunk is too short")
		}
		f.Code = binary.LittleEndian.Uint16(b[24:26])
	}
	switch {
	case f.Code != formatPCM && f.Code != formatFloat:
		return f, fmt.Errorf("WAV sample format %d is not PCM", f.Code)
	case f.Channels < 1 || f.SampleRate < 1 || f.BitsPerSample < 8:
		return f, errors.New("WAV format has no channels, rate or sample size")
	case f.BlockAlign != f.Channels*((f.BitsPerSample+7)/8):
		return f, errors.New("WAV block size does not match its channels and sample size")
	}
	return f, nil
}

// Cut writes the span from start to end of the WAV in r to w as a WAV
// of the same format, fading in and out over fade at its ends. The span
// is clamped to the audio; it returns the length written.
func Cut(r io.ReadSeeker, w io.Writer, start, end, fade time.Duration) (time.Duration, error) {
	h, err := readHeader(r)
	if err != nil {
		return 0, err
	}
	f := h.format
	frames := h.dataSize / int64(f.BlockAlign)
	first, last := toFrame(start, f.SampleRate), toFrame(end, f.SampleRate)
	first, last = max(first, 0), min(last, frames)
	if first >= last {
		return 0, fmt.Errorf("the span %s–%s is outside the audio", start, end)
	}
	data := make([]byte, (last-first)*int64(f.BlockAlign))
	if _, err := r.Seek(h.dataOffset+first*int64(f.BlockAlign), io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, fmt.Errorf("read wav samples: %w", err)
	}
	applyFades(data, f, int(toFrame(fade, f.SampleRate)))

	riffSize := 4 + 8 + len(h.fmtChunk) + 8 + len(data)
	if len(h.fmtChunk)%2 == 1 {
		riffSize++
	}
	if len(data)%2 == 1 {
		riffSize++
	}
	if int64(riffSize) > math.MaxUint32 {
		return 0, errors.New("the span is too long for a WAV file")
	}
	var out []byte
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(riffSize)) //nolint:gosec // checked above
	out = append(out, "WAVEfmt "...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(h.fmtChunk))) //nolint:gosec // at most 1024
	out = append(out, h.fmtChunk...)
	if len(h.fmtChunk)%2 == 1 {
		out = append(out, 0)
	}
	out = append(out, "data"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data))) //nolint:gosec // checked above
	if _, err := w.Write(out); err != nil {
		return 0, err
	}
	if _, err := w.Write(data); err != nil {
		return 0, err
	}
	if len(data)%2 == 1 {
		if _, err := w.Write([]byte{0}); err != nil {
			return 0, err
		}
	}
	return time.Duration(last-first) * time.Second / time.Duration(f.SampleRate), nil
}

func toFrame(t time.Duration, rate int) int64 {
	return int64(math.Round(t.Seconds() * float64(rate)))
}

// applyFades ramps the first and last n frames of data linearly from
// and to silence. Sample formats it cannot scale (8- and 24-bit) are
// left as they are.
func applyFades(data []byte, f Format, n int) {
	frames := len(data) / f.BlockAlign
	n = min(n, frames/2)
	if n <= 0 {
		return
	}
	size := f.BitsPerSample / 8
	scale := func(frame int, gain float64) {
		for c := range f.Channels {
			i := frame*f.BlockAlign + c*size
			switch {
			case f.Code == formatPCM && f.BitsPerSample == 16:
				v := int16(binary.LittleEndian.Uint16(data[i:])) //nolint:gosec // reinterpreting the sample's bits
				binary.LittleEndian.PutUint16(data[i:], uint16(int16(math.Round(float64(v)*gain)))) //nolint:gosec // same
			case f.Code == formatPCM && f.BitsPerSample == 32:
				v := int32(binary.LittleEndian.Uint32(data[i:])) //nolint:gosec // reinterpreting the sample's bits
				binary.LittleEndian.PutUint32(data[i:], uint32(int32(math.Round(float64(v)*gain)))) //nolint:gosec // same
			case f.Code == formatFloat && f.BitsPerSample == 32:
				v := math.Float32frombits(binary.LittleEndian.Uint32(data[i:]))
				binary.LittleEndian.PutUint32(data[i:], math.Float32bits(float32(float64(v)*gain)))
			}
		}
	}
	for k := range n {
		gain := float64(k) / float64(n)
		scale(k, gain)
		scale(frames-1-k, gain)
	}
}
