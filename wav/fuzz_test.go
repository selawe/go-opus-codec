package wav

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func FuzzNewReader(f *testing.F) {
	f.Add([]byte("RIFF$\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x02\x00\x80\xbb\x00\x00\x00\xee\x02\x00\x04\x00\x10\x00data\x00\x00\x00\x00"))
	f.Add([]byte{})
	f.Add([]byte("RIFF"))

	// One seed per format the reader understands, so mutations start inside the newer code:
	// 8/24/32-bit integer, 32-bit float, extensible, and RF64 with a ds64 size.
	chunks := func(fmtData, audio []byte) []wavChunk {
		return []wavChunk{{"fmt ", fmtData}, {"data", audio}}
	}
	audio := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	f.Add(buildWAVBytes("RIFF", chunks(buildFmtChunk(FormatPCM, 1, 48000, 8), audio)))
	f.Add(buildWAVBytes("RIFF", chunks(buildFmtChunk(FormatPCM, 2, 44100, 24), audio)))
	f.Add(buildWAVBytes("RIFF", chunks(buildFmtChunk(FormatPCM, 1, 48000, 32), audio)))
	f.Add(buildWAVBytes("RIFF", chunks(buildFmtChunk(FormatIEEEFloat, 1, 48000, 32), audio)))
	ds64 := make([]byte, 28)
	binary.LittleEndian.PutUint64(ds64[8:], uint64(len(audio)))
	f.Add(buildWAVBytes("RF64", []wavChunk{{"ds64", ds64}, {"fmt ", buildFmtChunk(FormatPCM, 1, 48000, 16)}, {"data", audio}}))

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := NewReader(bytes.NewReader(data))
		if err != nil {
			return
		}
		ch := r.Channels()
		if ch < 1 || r.SampleRate() < 1 || r.SampleRate() > MaxSampleRate {
			t.Fatalf("accepted a header with channels=%d rate=%d", ch, r.SampleRate())
		}

		// Both read paths must return whole frames, never more than asked, and terminate.
		pcm := make([]int16, 512)
		const maxReads = 1000
		for range maxReads {
			n, err := r.ReadInt16PCM(pcm)
			if n%ch != 0 || n > len(pcm) {
				t.Fatalf("ReadInt16PCM returned %d samples for %d channels", n, ch)
			}
			if err != nil {
				break
			}
		}

		r2, err := NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("second NewReader on identical input failed: %v", err)
		}
		fl := make([]float32, 512)
		for range maxReads {
			n, err := r2.ReadFloat32PCM(fl)
			if n%ch != 0 || n > len(fl) {
				t.Fatalf("ReadFloat32PCM returned %d samples for %d channels", n, ch)
			}
			for _, v := range fl[:n] {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
					t.Fatalf("non-finite sample %v", v)
				}
			}
			if err != nil {
				break
			}
		}
	})
}
