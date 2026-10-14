package opus

import (
	"errors"
	"math"
)

// Channel Order Conventions:
//
// 1. Vorbis / Opus RFC 7845 Family 1 (used by Downmix51ToStereo and Downmix71ToStereo):
//    - 5.1: [0: Front Left (FL), 1: Front Center (FC), 2: Front Right (FR),
//            3: Rear Left (RL),   4: Rear Right (RR),   5: LFE (subwoofer)]
//    - 7.1: [0: Front Left (FL), 1: Front Center (FC), 2: Front Right (FR),
//            3: Side Left (SL),   4: Side Right (SR),   5: Rear Left (RL),
//            6: Rear Right (RR),  7: LFE (subwoofer)]
//
// 2. Microsoft WAVE / SMPTE / ITU standard (used in multi-channel WAV files):
//    - 5.1: [0: Front Left (FL), 1: Front Right (FR), 2: Front Center (FC),
//            3: LFE,              4: Rear Left (RL),   5: Rear Right (RR)]
//    - 7.1: [0: Front Left (FL), 1: Front Right (FR), 2: Front Center (FC),
//            3: LFE,              4: Rear Left (RL),   5: Rear Right (RR),
//            6: Side Left (SL),   7: Side Right (SR)]
//
// If your input is from a WAV file or SMPTE stream, reorder it first with ConvertSMPTEToVorbis51
// (or its float32 variant) and then call Downmix51ToStereo or Downmix51ToStereoNormalized.

var (
	ErrInvalidSampleCount = errors.New("opus: sample count is not a multiple of channel count")
)

// ITU-R BS.775-1 downmix coefficient: 1/sqrt(2) ≈ 0.70710678.
const sqrt2Inv = 0.7071067811865475

// Normalization factor for 5.1: 1 / (1 + 2 * sqrt2Inv) = 1 / (1 + sqrt(2)) ≈ 0.41421356.
// Prevents hard clipping when all channels are driven at full scale.
const norm51 = 0.41421356237309503

// Normalization factor for 7.1. The left output sums FL (1) + FC (1/sqrt(2)) + SL (1/2) + RL (1/2) = 2 + 1/sqrt(2)
// ≈ 2.7071, more than the 5.1 sum (1 + 2/sqrt(2) ≈ 2.4142), so it needs its own factor: 1/2.7071 ≈ 0.36940.
// Reusing the 5.1 factor left a total gain of 1.12, which still clips at full scale.
const norm71 = 1 / (2 + sqrt2Inv)

func clampI16(val float64) int16 {
	if val > 32767.0 {
		return 32767
	}
	if val < -32768.0 {
		return -32768
	}
	return int16(math.Round(val))
}

func clampF32(val float32) float32 {
	if val > 1.0 {
		return 1.0
	}
	if val < -1.0 {
		return -1.0
	}
	return val
}

// DownmixStereoToMono converts stereo (2-channel) int16 PCM samples to mono (1-channel).
// If dst has sufficient capacity, it will be used; otherwise a new slice is allocated.
// The returned slice contains len(src)/2 samples.
func DownmixStereoToMono(dst []int16, src []int16) ([]int16, error) {
	if len(src)%2 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 2
	if cap(dst) >= numFrames {
		dst = dst[:numFrames]
	} else {
		dst = make([]int16, numFrames)
	}

	for i := 0; i < numFrames; i++ {
		l := int32(src[2*i])
		r := int32(src[2*i+1])
		dst[i] = int16((l + r) / 2)
	}
	return dst, nil
}

// DownmixStereoToMonoF32 converts stereo (2-channel) float32 PCM samples to mono (1-channel).
// If dst has sufficient capacity, it will be used; otherwise a new slice is allocated.
// The returned slice contains len(src)/2 samples.
func DownmixStereoToMonoF32(dst []float32, src []float32) ([]float32, error) {
	if len(src)%2 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 2
	if cap(dst) >= numFrames {
		dst = dst[:numFrames]
	} else {
		dst = make([]float32, numFrames)
	}

	for i := 0; i < numFrames; i++ {
		dst[i] = (src[2*i] + src[2*i+1]) * 0.5
	}
	return dst, nil
}

// Downmix51ToStereo downmixes 5.1 surround sound int16 PCM to stereo (2 channels: L, R)
// following un-normalized ITU-R BS.775-1 downmix equations:
//
//	L = FL + 0.7071*FC + 0.7071*RL
//	R = FR + 0.7071*FC + 0.7071*RR
//
// NOTE: Assumes Opus/Vorbis channel order [FL, FC, FR, RL, RR, LFE].
// For full-scale multi-channel audio where clipping must be avoided, use Downmix51ToStereoNormalized.
func Downmix51ToStereo(dst []int16, src []int16) ([]int16, error) {
	if len(src)%6 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 6
	outLen := numFrames * 2
	if cap(dst) >= outLen {
		dst = dst[:outLen]
	} else {
		dst = make([]int16, outLen)
	}

	for i := 0; i < numFrames; i++ {
		offset := i * 6
		fl := float64(src[offset])
		fc := float64(src[offset+1])
		fr := float64(src[offset+2])
		rl := float64(src[offset+3])
		rr := float64(src[offset+4])
		// LFE (src[offset+5]) is omitted per ITU-R BS.775 to prevent phase distortion and low-end clipping.

		centerSurround := fc * sqrt2Inv
		l := fl + centerSurround + rl*sqrt2Inv
		r := fr + centerSurround + rr*sqrt2Inv

		dst[2*i] = clampI16(l)
		dst[2*i+1] = clampI16(r)
	}
	return dst, nil
}

// Downmix51ToStereoNormalized downmixes 5.1 surround sound int16 PCM to stereo with 1/(1+sqrt(2))
// normalization (~0.414x) so that full-scale signals across all channels sum without clipping.
// Assumes Opus/Vorbis channel order [FL, FC, FR, RL, RR, LFE].
func Downmix51ToStereoNormalized(dst []int16, src []int16) ([]int16, error) {
	if len(src)%6 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 6
	outLen := numFrames * 2
	if cap(dst) >= outLen {
		dst = dst[:outLen]
	} else {
		dst = make([]int16, outLen)
	}

	const cFront = norm51
	const cOther = norm51 * sqrt2Inv

	for i := 0; i < numFrames; i++ {
		offset := i * 6
		fl := float64(src[offset])
		fc := float64(src[offset+1])
		fr := float64(src[offset+2])
		rl := float64(src[offset+3])
		rr := float64(src[offset+4])

		center := fc * cOther
		l := fl*cFront + center + rl*cOther
		r := fr*cFront + center + rr*cOther

		dst[2*i] = clampI16(l)
		dst[2*i+1] = clampI16(r)
	}
	return dst, nil
}

// Downmix51ToStereoF32 downmixes 5.1 surround sound float32 PCM to stereo (2 channels: L, R).
// Assumes Opus/Vorbis channel order [FL, FC, FR, RL, RR, LFE].
func Downmix51ToStereoF32(dst []float32, src []float32) ([]float32, error) {
	if len(src)%6 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 6
	outLen := numFrames * 2
	if cap(dst) >= outLen {
		dst = dst[:outLen]
	} else {
		dst = make([]float32, outLen)
	}

	const cSurr = float32(sqrt2Inv)

	for i := 0; i < numFrames; i++ {
		offset := i * 6
		fl := src[offset]
		fc := src[offset+1]
		fr := src[offset+2]
		rl := src[offset+3]
		rr := src[offset+4]

		centerSurround := fc * cSurr
		l := fl + centerSurround + rl*cSurr
		r := fr + centerSurround + rr*cSurr

		dst[2*i] = clampF32(l)
		dst[2*i+1] = clampF32(r)
	}
	return dst, nil
}

// Downmix51ToStereoNormalizedF32 downmixes 5.1 surround sound float32 PCM to stereo with 1/(1+sqrt(2))
// normalization (~0.414x) to guarantee output stays within [-1.0, 1.0] without clipping.
// Assumes Opus/Vorbis channel order [FL, FC, FR, RL, RR, LFE].
func Downmix51ToStereoNormalizedF32(dst []float32, src []float32) ([]float32, error) {
	if len(src)%6 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 6
	outLen := numFrames * 2
	if cap(dst) >= outLen {
		dst = dst[:outLen]
	} else {
		dst = make([]float32, outLen)
	}

	const cFront = float32(norm51)
	const cOther = float32(norm51 * sqrt2Inv)

	for i := 0; i < numFrames; i++ {
		offset := i * 6
		fl := src[offset]
		fc := src[offset+1]
		fr := src[offset+2]
		rl := src[offset+3]
		rr := src[offset+4]

		center := fc * cOther
		l := fl*cFront + center + rl*cOther
		r := fr*cFront + center + rr*cOther

		dst[2*i] = clampF32(l)
		dst[2*i+1] = clampF32(r)
	}
	return dst, nil
}

// Downmix71ToStereo downmixes 7.1 surround sound int16 PCM to stereo (2 channels: L, R).
// Assumes Opus/Vorbis channel order [FL, FC, FR, SL, SR, RL, RR, LFE].
func Downmix71ToStereo(dst []int16, src []int16) ([]int16, error) {
	if len(src)%8 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 8
	outLen := numFrames * 2
	if cap(dst) >= outLen {
		dst = dst[:outLen]
	} else {
		dst = make([]int16, outLen)
	}

	for i := 0; i < numFrames; i++ {
		offset := i * 8
		fl := float64(src[offset])
		fc := float64(src[offset+1])
		fr := float64(src[offset+2])
		sl := float64(src[offset+3])
		sr := float64(src[offset+4])
		rl := float64(src[offset+5])
		rr := float64(src[offset+6])

		surroundL := (sl + rl) * (sqrt2Inv * 0.7071)
		surroundR := (sr + rr) * (sqrt2Inv * 0.7071)
		center := fc * sqrt2Inv

		l := fl + center + surroundL
		r := fr + center + surroundR

		dst[2*i] = clampI16(l)
		dst[2*i+1] = clampI16(r)
	}
	return dst, nil
}

// Downmix71ToStereoNormalized downmixes 7.1 surround sound int16 PCM to stereo with normalization
// so that full-scale signals across all channels sum without clipping.
// Assumes Opus/Vorbis channel order [FL, FC, FR, SL, SR, RL, RR, LFE].
func Downmix71ToStereoNormalized(dst []int16, src []int16) ([]int16, error) {
	if len(src)%8 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 8
	outLen := numFrames * 2
	if cap(dst) >= outLen {
		dst = dst[:outLen]
	} else {
		dst = make([]int16, outLen)
	}

	const cFront = norm71
	const cCenter = norm71 * sqrt2Inv
	const cSurround = norm71 * 0.5

	for i := 0; i < numFrames; i++ {
		offset := i * 8
		fl := float64(src[offset])
		fc := float64(src[offset+1])
		fr := float64(src[offset+2])
		sl := float64(src[offset+3])
		sr := float64(src[offset+4])
		rl := float64(src[offset+5])
		rr := float64(src[offset+6])

		surroundL := (sl + rl) * cSurround
		surroundR := (sr + rr) * cSurround
		center := fc * cCenter

		l := fl*cFront + center + surroundL
		r := fr*cFront + center + surroundR

		dst[2*i] = clampI16(l)
		dst[2*i+1] = clampI16(r)
	}
	return dst, nil
}

// Downmix71ToStereoF32 downmixes 7.1 surround sound float32 PCM to stereo (2 channels: L, R).
// Assumes Opus/Vorbis channel order [FL, FC, FR, SL, SR, RL, RR, LFE].
func Downmix71ToStereoF32(dst []float32, src []float32) ([]float32, error) {
	if len(src)%8 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 8
	outLen := numFrames * 2
	if cap(dst) >= outLen {
		dst = dst[:outLen]
	} else {
		dst = make([]float32, outLen)
	}

	const cCenter = float32(sqrt2Inv)
	const cSurround = float32(sqrt2Inv * 0.7071)

	for i := 0; i < numFrames; i++ {
		offset := i * 8
		fl := src[offset]
		fc := src[offset+1]
		fr := src[offset+2]
		sl := src[offset+3]
		sr := src[offset+4]
		rl := src[offset+5]
		rr := src[offset+6]

		surroundL := (sl + rl) * cSurround
		surroundR := (sr + rr) * cSurround
		center := fc * cCenter

		l := fl + center + surroundL
		r := fr + center + surroundR

		dst[2*i] = clampF32(l)
		dst[2*i+1] = clampF32(r)
	}
	return dst, nil
}

// Downmix71ToStereoNormalizedF32 downmixes 7.1 surround sound float32 PCM to stereo with normalization
// to guarantee output stays within [-1.0, 1.0] without clipping.
// Assumes Opus/Vorbis channel order [FL, FC, FR, SL, SR, RL, RR, LFE].
func Downmix71ToStereoNormalizedF32(dst []float32, src []float32) ([]float32, error) {
	if len(src)%8 != 0 {
		return nil, ErrInvalidSampleCount
	}
	numFrames := len(src) / 8
	outLen := numFrames * 2
	if cap(dst) >= outLen {
		dst = dst[:outLen]
	} else {
		dst = make([]float32, outLen)
	}

	const cFront = float32(norm71)
	const cCenter = float32(norm71 * sqrt2Inv)
	const cSurround = float32(norm71 * 0.5)

	for i := 0; i < numFrames; i++ {
		offset := i * 8
		fl := src[offset]
		fc := src[offset+1]
		fr := src[offset+2]
		sl := src[offset+3]
		sr := src[offset+4]
		rl := src[offset+5]
		rr := src[offset+6]

		surroundL := (sl + rl) * cSurround
		surroundR := (sr + rr) * cSurround
		center := fc * cCenter

		l := fl*cFront + center + surroundL
		r := fr*cFront + center + surroundR

		dst[2*i] = clampF32(l)
		dst[2*i+1] = clampF32(r)
	}
	return dst, nil
}

// ConvertSMPTEToVorbis51 converts 5.1 channel ordering from Microsoft WAV/SMPTE
// [FL, FR, FC, LFE, RL, RR] to Vorbis/Opus RFC 7845 [FL, FC, FR, RL, RR, LFE].
func ConvertSMPTEToVorbis51(dst []int16, src []int16) ([]int16, error) {
	if len(src)%6 != 0 {
		return nil, ErrInvalidSampleCount
	}
	if cap(dst) >= len(src) {
		dst = dst[:len(src)]
	} else {
		dst = make([]int16, len(src))
	}

	numFrames := len(src) / 6
	for i := 0; i < numFrames; i++ {
		off := i * 6
		fl := src[off]
		fr := src[off+1]
		fc := src[off+2]
		lfe := src[off+3]
		rl := src[off+4]
		rr := src[off+5]

		dst[off] = fl
		dst[off+1] = fc
		dst[off+2] = fr
		dst[off+3] = rl
		dst[off+4] = rr
		dst[off+5] = lfe
	}
	return dst, nil
}

// ConvertSMPTEToVorbis51F32 converts 5.1 channel ordering from Microsoft WAV/SMPTE
// [FL, FR, FC, LFE, RL, RR] to Vorbis/Opus RFC 7845 [FL, FC, FR, RL, RR, LFE].
func ConvertSMPTEToVorbis51F32(dst []float32, src []float32) ([]float32, error) {
	if len(src)%6 != 0 {
		return nil, ErrInvalidSampleCount
	}
	if cap(dst) >= len(src) {
		dst = dst[:len(src)]
	} else {
		dst = make([]float32, len(src))
	}

	numFrames := len(src) / 6
	for i := 0; i < numFrames; i++ {
		off := i * 6
		fl := src[off]
		fr := src[off+1]
		fc := src[off+2]
		lfe := src[off+3]
		rl := src[off+4]
		rr := src[off+5]

		dst[off] = fl
		dst[off+1] = fc
		dst[off+2] = fr
		dst[off+3] = rl
		dst[off+4] = rr
		dst[off+5] = lfe
	}
	return dst, nil
}
