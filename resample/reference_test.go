package resample

// produceReference is the straightforward per-tap bounds-checked filter loop that produce()
// used before it was optimised; the optimised version must match it bit for bit.
func (r *Resampler) produceReference(final bool) []float32 {
	end := r.next
	if final {
		end = (r.inFrames*r.up + r.down - 1) / r.down
	} else {
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
	keepFrom := (r.next*r.down)/r.up - int64(r.half) + 1
	if drop := keepFrom - r.base; drop > 0 {
		drop = min(drop, r.inFrames-r.base)
		r.buf = append(r.buf[:0], r.buf[int(drop)*r.channels:]...)
		r.base += drop
	}
	return out
}
