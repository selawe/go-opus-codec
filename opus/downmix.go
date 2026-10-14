package opus

import (
	"errors"
	"math"
)

var (
	ErrInvalidSampleCount = errors.New("opus: sample count is not a multiple of channel count")
)

// ITU-R BS.775-1 downmix coefficient: 1/sqrt(2) ≈ 0.70710678.
const sqrt2Inv = 0.7071067811865475

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

// Downmix51ToStereo downmixes 5.1 surround sound (6 channels: FL, FC, FR, RL, RR, LFE per RFC 7845)
// int16 PCM to stereo (2 channels: L, R) following the ITU-R BS.775-1 downmix equations.
// Samples are clamped to [-32768, 32767].
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

// Downmix51ToStereoF32 downmixes 5.1 surround sound (6 channels: FL, FC, FR, RL, RR, LFE per RFC 7845)
// float32 PCM to stereo (2 channels: L, R) following the ITU-R BS.775-1 downmix equations.
// Samples are clamped to [-1.0, 1.0].
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

// Downmix71ToStereo downmixes 7.1 surround sound (8 channels: FL, FC, FR, SL, SR, RL, RR, LFE per RFC 7845)
// int16 PCM to stereo (2 channels: L, R) following ITU-R BS.775-1 principles.
// Samples are clamped to [-32768, 32767].
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
		// LFE (src[offset+7]) is omitted.

		// Rear and side surround each contribute half the surround energy (1/2 * sqrt(2)/2 or 0.5)
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

// Downmix71ToStereoF32 downmixes 7.1 surround sound (8 channels: FL, FC, FR, SL, SR, RL, RR, LFE per RFC 7845)
// float32 PCM to stereo (2 channels: L, R). Samples are clamped to [-1.0, 1.0].
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
