// Adapted from github.com/kazzmir/opus-go (package resample, PR #22 by James Riley Wilburn),
// BSD 3-Clause licensed like this project.

// Package resample converts PCM between sample rates - e.g. 44.1 kHz WAV
// to the 48 kHz Opus encodes at, since libopus only accepts
// 8/12/16/24/48 kHz input.
//
// It is a polyphase windowed-sinc (Blackman) filter. The output/input
// rate ratio is rational, so one table of phases covers every output
// sample exactly: no per-sample trig and no drift, however long the
// stream.
package resample

import (
	"fmt"
	"math"
)

// MaxTableEntries bounds the polyphase table (phases * taps) so a pathological
// rate pair, such as two large coprime rates from an untrusted header, cannot
// exhaust memory.
const MaxTableEntries = 1 << 22

// halfTaps is the filter half-width in input samples at the cutoff;
// 32 taps keeps the passband flat and aliasing far below 16-bit noise.
const halfTaps = 16

// Resampler converts one interleaved stream from one rate to another,
// incrementally: feed input with Process, then call Flush once at the
// end. Across the whole stream it produces exactly
// ceil(inputFrames * to / from) frames.
type Resampler struct {
	channels int
	up, down int64 // output/input rate ratio, reduced
	half     int
	taps     int
	table    []float32 // [phase][tap]

	partial  []float32 // incomplete trailing frame carried to the next Process
	buf      []float32 // pending input, interleaved; buf[0] is input frame base
	base     int64
	inFrames int64 // input frames seen so far
	next     int64 // next output frame to produce
}

// New returns a Resampler for interleaved audio with channels channels,
// from Hz to to Hz. It panics if channels, from or to is not positive; callers that
// take those values from untrusted input must validate them first.
func New(channels, from, to int) *Resampler {
	if err := Check(channels, from, to); err != nil {
		panic(err.Error())
	}
	g := gcd(from, to)
	up, down := to/g, from/g
	cutoff, half := filterShape(from, to)
	taps := 2 * half

	// table[p][j] weighs input frame base+j-half+1 for output phase p,
	// whose position lies p/up of the way past input frame base.
	table := make([]float32, up*taps)
	for p := range up {
		row := table[p*taps : (p+1)*taps]
		var sum float64
		for j := range taps {
			d := float64(j-half+1) - float64(p)/float64(up)
			u := d / float64(half)
			if u <= -1 || u >= 1 {
				continue
			}
			win := 0.42 + 0.5*math.Cos(math.Pi*u) + 0.08*math.Cos(2*math.Pi*u)
			v := cutoff * sinc(cutoff*d) * win
			row[j] = float32(v)
			sum += v
		}
		for j := range row { // unity gain at DC for every phase
			row[j] = float32(float64(row[j]) / sum)
		}
	}
	return &Resampler{
		channels: channels,
		up:       int64(up),
		down:     int64(down),
		half:     half,
		taps:     taps,
		table:    table,
	}
}

// Check reports whether New would accept channels, from and to without
// panicking or allocating an unreasonable filter table. Callers that take
// the rates from untrusted input should call it first.
func Check(channels, from, to int) error {
	if channels <= 0 || from <= 0 || to <= 0 {
		return fmt.Errorf("resample: channels and rates must be positive (channels=%d from=%d to=%d)", channels, from, to)
	}
	g := gcd(from, to)
	_, half := filterShape(from, to)
	if entries := float64(to/g) * float64(2*half); entries > MaxTableEntries {
		return fmt.Errorf("resample: rate pair %d -> %d needs a %.0f entry filter table (limit %d)", from, to, entries, MaxTableEntries)
	}
	return nil
}

// filterShape returns the cutoff: at the lower Nyquist, a little early for
// the window's transition band; and the filter half-width in input samples.
func filterShape(from, to int) (cutoff float64, half int) {
	cutoff = 0.97 * math.Min(1, float64(to)/float64(from))
	return cutoff, int(math.Ceil(halfTaps / cutoff))
}

// Process consumes interleaved input and returns every output frame whose
// filter window it now fully covers. A trailing partial frame (len(in) not a
// multiple of the channel count) is held back and completed by the next call.
func (r *Resampler) Process(in []float32) []float32 {
	if len(r.partial) > 0 {
		in = append(r.partial, in...)
		r.partial = nil
	}
	if rem := len(in) % r.channels; rem != 0 {
		r.partial = append([]float32(nil), in[len(in)-rem:]...)
		in = in[:len(in)-rem]
	}
	r.buf = append(r.buf, in...)
	r.inFrames += int64(len(in) / r.channels)
	return r.produce(false)
}

// Flush ends the stream (treating input past its end as silence) and
// returns the remaining output.
func (r *Resampler) Flush() []float32 {
	return r.produce(true)
}

// ProcessInt16 is Process for 16-bit PCM.
func (r *Resampler) ProcessInt16(in []int16) []int16 {
	f := make([]float32, len(in))
	for i, v := range in {
		f[i] = float32(v) / 32768
	}
	return toInt16(r.Process(f))
}

// FlushInt16 is Flush for 16-bit PCM.
func (r *Resampler) FlushInt16() []int16 {
	return toInt16(r.Flush())
}

func (r *Resampler) produce(final bool) []float32 {
	end := r.next // one past the last output frame to produce
	if final {
		end = (r.inFrames*r.up + r.down - 1) / r.down
	} else {
		// Output n needs input frames up to n*down/up + half.
		for ; ; end++ {
			if (end*r.down)/r.up+int64(r.half) >= r.inFrames {
				break
			}
		}
	}
	out := make([]float32, 0, int(end-r.next)*r.channels)
	for ; r.next < end; r.next++ {
		pos := r.next * r.down
		center := pos / r.up
		row := r.table[int(pos%r.up)*r.taps:][:r.taps]

		// Interior fast path: the whole filter window lies inside the stream, so no tap
		// needs a bounds check. Taps are still summed in ascending order per channel, so
		// the result is bit-identical to the edge path below.
		if first := center - int64(r.half) + 1; first >= 0 && first+int64(r.taps) <= r.inFrames {
			win := r.buf[int(first-r.base)*r.channels:][:r.taps*r.channels]
			switch r.channels {
			case 1:
				win = win[:len(row)]
				var acc float32
				for j, w := range row {
					acc += w * win[j]
				}
				out = append(out, acc)
			case 2:
				win = win[:2*len(row)]
				var a0, a1 float32
				for j, w := range row {
					a0 += w * win[2*j]
					a1 += w * win[2*j+1]
				}
				out = append(out, a0, a1)
			default:
				for c := range r.channels {
					var acc float32
					for j, w := range row {
						acc += w * win[j*r.channels+c]
					}
					out = append(out, acc)
				}
			}
			continue
		}

		for c := range r.channels {
			var acc float32
			for j, w := range row {
				k := center + int64(j-r.half+1)
				if k < 0 || k >= r.inFrames {
					continue
				}
				acc += w * r.buf[int(k-r.base)*r.channels+c]
			}
			out = append(out, acc)
		}
	}
	// Drop input no future output frame reaches back to.
	keepFrom := (r.next*r.down)/r.up - int64(r.half) + 1
	if drop := keepFrom - r.base; drop > 0 {
		drop = min(drop, r.inFrames-r.base)
		r.buf = append(r.buf[:0], r.buf[int(drop)*r.channels:]...)
		r.base += drop
	}
	return out
}

// Int16 resamples a whole interleaved 16-bit buffer in one call.
func Int16(in []int16, channels, from, to int) []int16 {
	r := New(channels, from, to)
	return append(r.ProcessInt16(in), r.FlushInt16()...)
}

func toInt16(in []float32) []int16 {
	out := make([]int16, len(in))
	for i, v := range in {
		s := math.Round(float64(v) * 32768)
		if s != s { // NaN has no defined int16 conversion; emit silence
			s = 0
		}
		out[i] = int16(max(-32768, min(32767, s)))
	}
	return out
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
