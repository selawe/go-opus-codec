package resample

import (
	"math"
	"testing"
)

// FuzzResample checks that any rate pair Check accepts can be constructed and run, that the
// output length matches the documented ceil(inputFrames * to / from), and that arbitrary
// float input (NaN, Inf, huge values) never crashes the int16 conversion.
func FuzzResample(f *testing.F) {
	f.Add(uint8(2), uint32(44100), uint32(48000), []byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Add(uint8(1), uint32(96000), uint32(48000), []byte{0xFF, 0xFF, 0xC0, 0x7F})
	f.Add(uint8(1), uint32(700001), uint32(48000), []byte{1, 2})

	f.Fuzz(func(t *testing.T, channels uint8, from, to uint32, raw []byte) {
		ch, fr, tt := int(channels%8)+1, int(from%1_000_000)+1, int(to%1_000_000)+1
		if err := Check(ch, fr, tt); err != nil {
			return
		}
		// Check allows tables of millions of entries; keep each fuzz execution cheap.
		if _, half := filterShape(fr, tt); float64(tt/gcd(fr, tt))*float64(2*half) > 1<<16 {
			return
		}
		in := make([]int16, len(raw)/2/ch*ch)
		for i := range in {
			in[i] = int16(uint16(raw[2*i]) | uint16(raw[2*i+1])<<8)
		}

		r := New(ch, fr, tt)
		out := append(r.ProcessInt16(in), r.FlushInt16()...)

		frames := len(in) / ch
		want := int(math.Ceil(float64(frames) * float64(tt) / float64(fr)))
		if got := len(out) / ch; got != want {
			t.Fatalf("%d ch %d->%d Hz: %d frames in gave %d out, want %d", ch, fr, tt, frames, got, want)
		}

		// Raw float input, including non-finite values.
		r2 := New(ch, fr, tt)
		fin := make([]float32, len(in))
		for i := range fin {
			fin[i] = math.Float32frombits(uint32(raw[i%len(raw)])<<24 | uint32(i))
		}
		_ = toInt16(append(r2.Process(fin), r2.Flush()...))
	})
}
