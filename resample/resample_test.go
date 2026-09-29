// Adapted from github.com/kazzmir/opus-go (package resample, PR #22 by James Riley Wilburn),
// BSD 3-Clause licensed like this project.

package resample

import (
	"math"
	"slices"
	"testing"
)

func sine(rate, channels, frames int, hz float64) []float32 {
	s := make([]float32, frames*channels)
	for i := range frames {
		v := float32(0.5 * math.Sin(2*math.Pi*hz*float64(i)/float64(rate)))
		for c := range channels {
			s[i*channels+c] = v
		}
	}
	return s
}

// pitch estimates channel 0's frequency from zero crossings, ignoring the
// filter's edge transients.
func pitch(s []float32, channels, rate int) float64 {
	frames := len(s) / channels
	lo, hi := frames/10, frames*9/10
	n := 0
	for i := lo + 1; i < hi; i++ {
		if (s[(i-1)*channels] < 0) != (s[i*channels] < 0) {
			n++
		}
	}
	return float64(n) / 2 / (float64(hi-lo) / float64(rate))
}

func TestRates(t *testing.T) {
	const hz = 440
	for _, tc := range []struct{ from, to, channels, frames int }{
		{44100, 48000, 2, 44100},
		{22050, 48000, 1, 22050 + 17},
		{48000, 16000, 1, 48000},
		{32000, 48000, 2, 1},
		{44100, 48000, 1, 0},
	} {
		in := sine(tc.from, tc.channels, tc.frames, hz)
		r := New(tc.channels, tc.from, tc.to)
		out := append(r.Process(in), r.Flush()...)
		wantFrames := (tc.frames*tc.to + tc.from - 1) / tc.from
		if got := len(out) / tc.channels; got != wantFrames {
			t.Errorf("%+v: %d output frames, want %d", tc, got, wantFrames)
		}
		if tc.frames < tc.from/2 {
			continue
		}
		if got := pitch(out, tc.channels, tc.to); math.Abs(got-hz) > 2 {
			t.Errorf("%+v: pitch %.1f Hz, want %d", tc, got, hz)
		}
		// Steady-state amplitude is preserved (unity passband gain).
		var peak float32
		for _, v := range out[len(out)/4 : len(out)*3/4] {
			peak = max(peak, v)
		}
		if math.Abs(float64(peak)-0.5) > 0.01 {
			t.Errorf("%+v: peak %.3f, want 0.5", tc, peak)
		}
	}
}

// TestStreamingMatchesOneShot feeds input in uneven chunks and checks the
// output is identical to resampling it all at once.
func TestStreamingMatchesOneShot(t *testing.T) {
	in := sine(44100, 2, 30000, 1000)
	r := New(2, 44100, 48000)
	want := append(r.Process(in), r.Flush()...)

	r = New(2, 44100, 48000)
	var got []float32
	for off, step := 0, 1; off < len(in); step = step*3%997 + 1 {
		end := min(off+step*2, len(in))
		got = append(got, r.Process(in[off:end])...)
		off = end
	}
	got = append(got, r.Flush()...)
	if !slices.Equal(got, want) {
		t.Fatalf("streamed output (%d samples) differs from one-shot (%d)", len(got), len(want))
	}
}

func TestInt16(t *testing.T) {
	in := make([]int16, 4410)
	for i := range in {
		in[i] = int16(10000 * math.Sin(2*math.Pi*300*float64(i)/44100))
	}
	out := Int16(in, 1, 44100, 48000)
	if len(out) != 4800 {
		t.Fatalf("%d samples, want 4800", len(out))
	}
}

// rms returns the RMS of the middle half of s, away from the filter's edge transients.
func rms(s []float32) float64 {
	mid := s[len(s)/4 : len(s)*3/4]
	var sum float64
	for _, v := range mid {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(mid)))
}

// Content above the target Nyquist frequency must be strongly attenuated instead of
// aliasing back into the audible band.
func TestDownsamplingSuppressesAliasing(t *testing.T) {
	const from, to = 48000, 16000
	for _, hz := range []float64{9000, 12000, 20000} { // all above the 8 kHz Nyquist of 16 kHz
		in := sine(from, 1, from, hz)
		r := New(1, from, to)
		out := append(r.Process(in), r.Flush()...)
		if att := 20 * math.Log10(rms(out)/rms(in)); att > -40 {
			t.Errorf("%.0f Hz tone attenuated by only %.1f dB when downsampling to %d Hz, want at least 40 dB", hz, -att, to)
		}
	}
}

func TestPassbandIsFlatWhenUpsampling(t *testing.T) {
	for _, hz := range []float64{100, 1000, 8000, 16000} {
		in := sine(44100, 1, 44100, hz)
		r := New(1, 44100, 48000)
		out := append(r.Process(in), r.Flush()...)
		if gain := 20 * math.Log10(rms(out)/rms(in)); math.Abs(gain) > 0.1 {
			t.Errorf("%.0f Hz tone changed level by %.2f dB, want within 0.1 dB", hz, gain)
		}
	}
}

func TestChannelsAreIndependent(t *testing.T) {
	const frames = 4410
	in := make([]float32, frames*2)
	for i := range frames { // left: tone, right: silence
		in[2*i] = float32(0.5 * math.Sin(2*math.Pi*500*float64(i)/44100))
	}
	r := New(2, 44100, 48000)
	out := append(r.Process(in), r.Flush()...)
	for i := 1; i < len(out); i += 2 {
		if out[i] != 0 {
			t.Fatalf("right channel leaked signal at frame %d: %v", i/2, out[i])
		}
	}
}

func TestNewPanicsOnInvalidArguments(t *testing.T) {
	for _, a := range [][3]int{{0, 44100, 48000}, {2, 0, 48000}, {2, 44100, 0}, {-1, 44100, 48000}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("New(%v) did not panic", a)
				}
			}()
			New(a[0], a[1], a[2])
		}()
	}
}

func BenchmarkResample44100To48000Stereo(b *testing.B) {
	in := sine(44100, 2, 44100, 440) // one second per iteration
	b.SetBytes(int64(len(in)) * 4)
	b.ReportAllocs()
	for b.Loop() {
		r := New(2, 44100, 48000)
		_ = r.Process(in)
		_ = r.Flush()
	}
}

// The optimised filter loop must produce exactly the same samples as the simple reference
// loop for every rate pair, channel count and chunking, including the stream edges.
func TestOptimisedFilterMatchesReference(t *testing.T) {
	for _, tc := range []struct{ from, to, channels, frames int }{
		{44100, 48000, 2, 5000},
		{22050, 48000, 1, 3001},
		{48000, 16000, 2, 7000},
		{8000, 48000, 1, 700},
		{32000, 48000, 2, 5}, // shorter than the filter window
		{44100, 48000, 1, 0}, // empty
		{48000, 44100, 2, 6001},
	} {
		in := sine(tc.from, tc.channels, tc.frames, 1234)
		fast, ref := New(tc.channels, tc.from, tc.to), New(tc.channels, tc.from, tc.to)
		var gotFast, gotRef []float32
		for off, step := 0, 1; off < len(in); step = step*7%501 + 1 {
			end := min(off+step*tc.channels, len(in))
			gotFast = append(gotFast, fast.Process(in[off:end])...)
			ref.buf = append(ref.buf, in[off:end]...)
			ref.inFrames += int64((end - off) / tc.channels)
			gotRef = append(gotRef, ref.produceReference(false)...)
			off = end
		}
		gotFast = append(gotFast, fast.Flush()...)
		gotRef = append(gotRef, ref.produceReference(true)...)
		if !slices.Equal(gotFast, gotRef) {
			t.Errorf("%+v: optimised output differs from the reference (%d vs %d samples)", tc, len(gotFast), len(gotRef))
		}
	}
}
