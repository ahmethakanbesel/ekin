// Package mp4 reads the length of an MP4 (ISO base media) file from its
// movie header, without decoding any media.
package mp4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// maxBoxes bounds the walk, so a malformed file cannot loop it.
const maxBoxes = 1024

// Duration returns the presentation length recorded in moov/mvhd. The
// movie box may come before or after the media data.
func Duration(r io.ReadSeeker) (time.Duration, error) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	moov, err := find(r, 0, end, "moov")
	if err != nil {
		return 0, err
	}
	mvhd, err := find(r, moov.body, moov.end, "mvhd")
	if err != nil {
		return 0, err
	}
	return movieHeader(r, mvhd)
}

type box struct {
	body, end int64 // the payload's offset and the box's end
}

// find returns the first box of kind among the siblings in [from, to).
func find(r io.ReadSeeker, from, to int64, kind string) (box, error) {
	at := from
	for range maxBoxes {
		if at+8 > to {
			return box{}, fmt.Errorf("mp4: no %s box", kind)
		}
		if _, err := r.Seek(at, io.SeekStart); err != nil {
			return box{}, err
		}
		var head [16]byte
		if _, err := io.ReadFull(r, head[:8]); err != nil {
			return box{}, fmt.Errorf("mp4: read box header: %w", err)
		}
		size, header := int64(binary.BigEndian.Uint32(head[:4])), int64(8)
		switch size {
		case 0: // runs to the end of its parent
			size = to - at
		case 1: // a 64-bit size follows the type
			if _, err := io.ReadFull(r, head[8:16]); err != nil {
				return box{}, fmt.Errorf("mp4: read box size: %w", err)
			}
			large := binary.BigEndian.Uint64(head[8:16])
			if large > math.MaxInt64 {
				return box{}, errors.New("mp4: box too large")
			}
			size, header = int64(large), 16
		}
		if size < header || at+size > to {
			return box{}, errors.New("mp4: malformed box size")
		}
		if string(head[4:8]) == kind {
			return box{body: at + header, end: at + size}, nil
		}
		at += size
	}
	return box{}, errors.New("mp4: too many boxes")
}

// movieHeader reads the timescale and duration of an mvhd box.
func movieHeader(r io.ReadSeeker, b box) (time.Duration, error) {
	if _, err := r.Seek(b.body, io.SeekStart); err != nil {
		return 0, err
	}
	var buf [32]byte
	if _, err := io.ReadFull(r, buf[:4]); err != nil {
		return 0, fmt.Errorf("mp4: read mvhd: %w", err)
	}
	var timescale, duration uint64
	switch version := buf[0]; version {
	case 0: // 32-bit creation, modification, timescale, duration
		if b.end-b.body < 20 {
			return 0, errors.New("mp4: short mvhd")
		}
		if _, err := io.ReadFull(r, buf[4:20]); err != nil {
			return 0, fmt.Errorf("mp4: read mvhd: %w", err)
		}
		timescale = uint64(binary.BigEndian.Uint32(buf[12:16]))
		duration = uint64(binary.BigEndian.Uint32(buf[16:20]))
		if duration == math.MaxUint32 {
			duration = 0 // "unknown"
		}
	case 1: // 64-bit creation and modification, 32-bit timescale, 64-bit duration
		if b.end-b.body < 32 {
			return 0, errors.New("mp4: short mvhd")
		}
		if _, err := io.ReadFull(r, buf[4:32]); err != nil {
			return 0, fmt.Errorf("mp4: read mvhd: %w", err)
		}
		timescale = uint64(binary.BigEndian.Uint32(buf[20:24]))
		duration = binary.BigEndian.Uint64(buf[24:32])
		if duration == math.MaxUint64 {
			duration = 0
		}
	default:
		return 0, fmt.Errorf("mp4: unknown mvhd version %d", version)
	}
	if timescale == 0 || duration == 0 {
		return 0, errors.New("mp4: the movie header records no length")
	}
	secs, rest := duration/timescale, duration%timescale
	if secs > uint64(math.MaxInt64/int64(time.Second)) {
		return 0, errors.New("mp4: length out of range")
	}
	// rest < timescale < 2^32, so rest*1e9 fits in 64 bits and the
	// fraction is under a second.
	frac := time.Duration(rest * uint64(time.Second) / timescale) //nolint:gosec // < 1e9, see above
	return time.Duration(secs)*time.Second + frac, nil
}
