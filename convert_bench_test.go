package opusgo

import (
	"bytes"
	"testing"
)

// The end-to-end encode path: WAV reading, optional resampling, framing, encoding and Ogg writing.
func benchmarkEncodeWAV(b *testing.B, rate int) {
	wavData := generateSineWAV(&testing.T{}, rate, 2, rate*10) // ten seconds of stereo
	opts := &EncodeOptions{Complexity: 3, ComplexityExplicit: true, Serial: 1, Comments: []string{"A=B"}}
	b.SetBytes(int64(len(wavData)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &out, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeWAV_48k(b *testing.B)  { benchmarkEncodeWAV(b, 48000) }
func BenchmarkEncodeWAV_44k1(b *testing.B) { benchmarkEncodeWAV(b, 44100) }
func BenchmarkEncodeWAV_16k(b *testing.B)  { benchmarkEncodeWAV(b, 16000) }

func BenchmarkDecodeToWAV(b *testing.B) {
	wavData := generateSineWAV(&testing.T{}, 48000, 2, 48000*10)
	var enc bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &enc, &EncodeOptions{Serial: 1}); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(wavData)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := DecodeOggOpusToWAV(bytes.NewReader(enc.Bytes()), &discardSeeker{}); err != nil {
			b.Fatal(err)
		}
	}
}

// discardSeeker is an io.WriteSeeker that keeps nothing, so a benchmark measures the library's own
// allocations rather than those of an in-memory sink.
type discardSeeker struct{ pos, size int64 }

func (d *discardSeeker) Write(p []byte) (int, error) {
	d.pos += int64(len(p))
	d.size = max(d.size, d.pos)
	return len(p), nil
}

func (d *discardSeeker) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case 0:
		d.pos = off
	case 1:
		d.pos += off
	case 2:
		d.pos = d.size + off
	}
	return d.pos, nil
}
