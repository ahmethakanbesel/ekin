package mp4_test

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"ekin/pkg/mp4"
)

func boxOf(kind string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(out, kind...), body...)
}

// largeBoxOf uses the 64-bit size form.
func largeBoxOf(kind string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := binary.BigEndian.AppendUint32(nil, 1)
	out = append(out, kind...)
	out = binary.BigEndian.AppendUint64(out, uint64(16+len(body)))
	return append(out, body...)
}

func mvhd0(timescale, duration uint32) []byte {
	b := []byte{0, 0, 0, 0}                 // version 0, flags
	b = binary.BigEndian.AppendUint32(b, 1) // creation
	b = binary.BigEndian.AppendUint32(b, 2) // modification
	b = binary.BigEndian.AppendUint32(b, timescale)
	b = binary.BigEndian.AppendUint32(b, duration)
	return boxOf("mvhd", b, make([]byte, 80))
}

func mvhd1(timescale uint32, duration uint64) []byte {
	b := []byte{1, 0, 0, 0}
	b = binary.BigEndian.AppendUint64(b, 1)
	b = binary.BigEndian.AppendUint64(b, 2)
	b = binary.BigEndian.AppendUint32(b, timescale)
	b = binary.BigEndian.AppendUint64(b, duration)
	return boxOf("mvhd", b, make([]byte, 80))
}

func TestDuration(t *testing.T) {
	ftyp := boxOf("ftyp", []byte("isom\x00\x00\x02\x00isomiso2mp41"))
	mdat := boxOf("mdat", make([]byte, 4096))
	trak := boxOf("trak", boxOf("tkhd", make([]byte, 84)))
	tests := []struct {
		name string
		file []byte
		want time.Duration
		ok   bool
	}{
		{"faststart", bytes.Join([][]byte{ftyp, boxOf("moov", mvhd0(1000, 47520), trak), mdat}, nil), 47520 * time.Millisecond, true},
		{"moov after mdat", bytes.Join([][]byte{ftyp, mdat, boxOf("moov", mvhd0(1000, 47520))}, nil), 47520 * time.Millisecond, true},
		{"mvhd after another child", bytes.Join([][]byte{ftyp, boxOf("moov", trak, mvhd0(90000, 45*90000))}, nil), 45 * time.Second, true},
		{"version 1", bytes.Join([][]byte{ftyp, boxOf("moov", mvhd1(44100, 8*60*44100+10*44100+22050))}, nil), 8*time.Minute + 10500*time.Millisecond, true},
		{"64-bit box size", bytes.Join([][]byte{ftyp, largeBoxOf("moov", mvhd0(600, 1800))}, nil), 3 * time.Second, true},
		{"odd timescale keeps sub-second precision", bytes.Join([][]byte{boxOf("moov", mvhd0(30000, 1001))}, nil), 33366666 * time.Nanosecond, true},
		{"no moov", bytes.Join([][]byte{ftyp, mdat}, nil), 0, false},
		{"no mvhd", bytes.Join([][]byte{ftyp, boxOf("moov", trak)}, nil), 0, false},
		{"zero timescale", boxOf("moov", mvhd0(0, 100)), 0, false},
		{"unknown length", boxOf("moov", mvhd0(1000, 0xFFFFFFFF)), 0, false},
		{"box larger than the file", append(binary.BigEndian.AppendUint32(nil, 1<<20), "moov"...), 0, false},
		{"box smaller than its header", append(binary.BigEndian.AppendUint32(nil, 4), "moov"...), 0, false},
		{"truncated mvhd", boxOf("moov", boxOf("mvhd", []byte{0, 0, 0, 0, 1, 2})), 0, false},
		{"empty", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mp4.Duration(bytes.NewReader(tt.file))
			if (err == nil) != tt.ok {
				t.Fatalf("err = %v, want ok %v", err, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("duration = %v, want %v", got, tt.want)
			}
		})
	}
}
