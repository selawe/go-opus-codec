package opus

import (
	"errors"
	"math"
	"testing"
)

func TestDownmixStereoToMono(t *testing.T) {
	// 2 frames of stereo: [L1, R1, L2, R2]
	stereo := []int16{1000, 2000, -1000, -3000}
	mono, err := DownmixStereoToMono(nil, stereo)
	if err != nil {
		t.Fatalf("DownmixStereoToMono failed: %v", err)
	}
	if len(mono) != 2 {
		t.Fatalf("expected 2 mono samples, got %d", len(mono))
	}
	if mono[0] != 1500 {
		t.Errorf("expected mono[0] = 1500, got %d", mono[0])
	}
	if mono[1] != -2000 {
		t.Errorf("expected mono[1] = -2000, got %d", mono[1])
	}

	// Odd length error check
	_, err = DownmixStereoToMono(nil, []int16{1, 2, 3})
	if !errors.Is(err, ErrInvalidSampleCount) {
		t.Errorf("expected ErrInvalidSampleCount, got %v", err)
	}

	// Buffer reuse check
	dstBuf := make([]int16, 2)
	mono2, err := DownmixStereoToMono(dstBuf, stereo)
	if err != nil || &mono2[0] != &dstBuf[0] {
		t.Errorf("expected buffer reuse for dst")
	}
}

func TestDownmixStereoToMonoF32(t *testing.T) {
	stereo := []float32{0.4, 0.6, -0.2, -0.8}
	mono, err := DownmixStereoToMonoF32(nil, stereo)
	if err != nil {
		t.Fatalf("DownmixStereoToMonoF32 failed: %v", err)
	}
	if math.Abs(float64(mono[0]-0.5)) > 1e-5 {
		t.Errorf("expected mono[0] ≈ 0.5, got %f", mono[0])
	}
	if math.Abs(float64(mono[1]-(-0.5))) > 1e-5 {
		t.Errorf("expected mono[1] ≈ -0.5, got %f", mono[1])
	}

	_, err = DownmixStereoToMonoF32(nil, []float32{1.0})
	if !errors.Is(err, ErrInvalidSampleCount) {
		t.Errorf("expected ErrInvalidSampleCount, got %v", err)
	}
}

func TestDownmix51ToStereo(t *testing.T) {
	// 5.1 layout: [FL, FC, FR, RL, RR, LFE]
	// Test pure Center channel (should appear equally in L and R scaled by sqrt2Inv)
	src := []int16{0, 10000, 0, 0, 0, 9999}
	stereo, err := Downmix51ToStereo(nil, src)
	if err != nil {
		t.Fatalf("Downmix51ToStereo: %v", err)
	}
	if len(stereo) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(stereo))
	}
	expected := int16(math.Round(10000.0 * sqrt2Inv)) // ~7071
	if stereo[0] != expected || stereo[1] != expected {
		t.Errorf("expected (%d, %d), got (%d, %d)", expected, expected, stereo[0], stereo[1])
	}

	// Test LFE is ignored and front channels preserved
	src2 := []int16{5000, 0, 8000, 0, 0, 32767}
	stereo2, err := Downmix51ToStereo(nil, src2)
	if err != nil {
		t.Fatalf("Downmix51ToStereo: %v", err)
	}
	if stereo2[0] != 5000 || stereo2[1] != 8000 {
		t.Errorf("expected (5000, 8000), got (%d, %d)", stereo2[0], stereo2[1])
	}

	// Test clamping on loud input
	loud := []int16{30000, 30000, 30000, 30000, 30000, 0}
	stereoClamped, err := Downmix51ToStereo(nil, loud)
	if err != nil {
		t.Fatalf("Downmix51ToStereo: %v", err)
	}
	if stereoClamped[0] != 32767 || stereoClamped[1] != 32767 {
		t.Errorf("expected clamped 32767, got (%d, %d)", stereoClamped[0], stereoClamped[1])
	}

	// Invalid sample count
	_, err = Downmix51ToStereo(nil, []int16{1, 2, 3, 4, 5})
	if !errors.Is(err, ErrInvalidSampleCount) {
		t.Errorf("expected ErrInvalidSampleCount, got %v", err)
	}
}

func TestDownmix51ToStereoF32(t *testing.T) {
	src := []float32{0.5, 0.0, 0.25, 0.0, 0.0, 0.9}
	stereo, err := Downmix51ToStereoF32(nil, src)
	if err != nil {
		t.Fatalf("Downmix51ToStereoF32: %v", err)
	}
	if math.Abs(float64(stereo[0]-0.5)) > 1e-5 || math.Abs(float64(stereo[1]-0.25)) > 1e-5 {
		t.Errorf("expected (0.5, 0.25), got (%f, %f)", stereo[0], stereo[1])
	}

	// Test clamping
	loud := []float32{1.0, 1.0, 1.0, 1.0, 1.0, 0.0}
	stereoLoud, err := Downmix51ToStereoF32(nil, loud)
	if err != nil {
		t.Fatalf("Downmix51ToStereoF32: %v", err)
	}
	if stereoLoud[0] != 1.0 || stereoLoud[1] != 1.0 {
		t.Errorf("expected clamped 1.0, got (%f, %f)", stereoLoud[0], stereoLoud[1])
	}
}

func TestDownmix71ToStereo(t *testing.T) {
	// 7.1 layout: [FL, FC, FR, SL, SR, RL, RR, LFE]
	src := []int16{1000, 0, 2000, 0, 0, 0, 0, 30000}
	stereo, err := Downmix71ToStereo(nil, src)
	if err != nil {
		t.Fatalf("Downmix71ToStereo: %v", err)
	}
	if len(stereo) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(stereo))
	}
	if stereo[0] != 1000 || stereo[1] != 2000 {
		t.Errorf("expected (1000, 2000), got (%d, %d)", stereo[0], stereo[1])
	}

	// Invalid length
	_, err = Downmix71ToStereo(nil, []int16{1, 2, 3, 4, 5, 6, 7})
	if !errors.Is(err, ErrInvalidSampleCount) {
		t.Errorf("expected ErrInvalidSampleCount, got %v", err)
	}
}

func TestDownmix71ToStereoF32(t *testing.T) {
	src := []float32{0.2, 0.0, 0.4, 0.0, 0.0, 0.0, 0.0, 0.8}
	stereo, err := Downmix71ToStereoF32(nil, src)
	if err != nil {
		t.Fatalf("Downmix71ToStereoF32: %v", err)
	}
	if math.Abs(float64(stereo[0]-0.2)) > 1e-5 || math.Abs(float64(stereo[1]-0.4)) > 1e-5 {
		t.Errorf("expected (0.2, 0.4), got (%f, %f)", stereo[0], stereo[1])
	}
}

func TestDownmix51ToStereoNormalized_NoClipping(t *testing.T) {
	// Full-scale in every channel: 32767
	fullScale := []int16{32767, 32767, 32767, 32767, 32767, 32767}
	stereo, err := Downmix51ToStereoNormalized(nil, fullScale)
	if err != nil {
		t.Fatalf("Downmix51ToStereoNormalized: %v", err)
	}

	// Total sum coefficient is exactly 1.0 (0.4142 + 2*0.29289 = 1.0); an int16 cannot exceed 32767, so
	// "no clipping" is checked with a half-scale input in TestDownmixNormalizedGain.
	// Result should be approximately 32767
	if math.Abs(float64(stereo[0]-32767)) > 2 {
		t.Errorf("expected ~32767, got %d", stereo[0])
	}

	// Float32 full-scale
	fullScaleF32 := []float32{1.0, 1.0, 1.0, 1.0, 1.0, 1.0}
	stereoF32, err := Downmix51ToStereoNormalizedF32(nil, fullScaleF32)
	if err != nil {
		t.Fatalf("Downmix51ToStereoNormalizedF32: %v", err)
	}
	if stereoF32[0] > 1.0001 || stereoF32[1] > 1.0001 {
		t.Errorf("expected normalized float output <= 1.0, got (%f, %f)", stereoF32[0], stereoF32[1])
	}
}

func TestDownmix71ToStereoNormalized_NoClipping(t *testing.T) {
	fullScale := []int16{32767, 32767, 32767, 32767, 32767, 32767, 32767, 32767}
	if _, err := Downmix71ToStereoNormalized(nil, fullScale); err != nil {
		t.Fatalf("Downmix71ToStereoNormalized: %v", err)
	}

	fullScaleF32 := []float32{1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0}
	stereoF32, err := Downmix71ToStereoNormalizedF32(nil, fullScaleF32)
	if err != nil {
		t.Fatalf("Downmix71ToStereoNormalizedF32: %v", err)
	}
	if stereoF32[0] > 1.0001 || stereoF32[1] > 1.0001 {
		t.Errorf("expected normalized 7.1 float output <= 1.0, got (%f, %f)", stereoF32[0], stereoF32[1])
	}
}

func TestConvertSMPTEToVorbis51(t *testing.T) {
	// SMPTE: [0:FL, 1:FR, 2:FC, 3:LFE, 4:RL, 5:RR]
	smpte := []int16{1, 2, 3, 4, 5, 6}
	vorbis, err := ConvertSMPTEToVorbis51(nil, smpte)
	if err != nil {
		t.Fatalf("ConvertSMPTEToVorbis51: %v", err)
	}
	// Expected Vorbis: [0:FL, 1:FC, 2:FR, 3:RL, 4:RR, 5:LFE] -> [1, 3, 2, 5, 6, 4]
	expected := []int16{1, 3, 2, 5, 6, 4}
	for i, v := range expected {
		if vorbis[i] != v {
			t.Errorf("channel %d: got %d, want %d", i, vorbis[i], v)
		}
	}

	// Float32 conversion
	smpteF32 := []float32{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}
	vorbisF32, err := ConvertSMPTEToVorbis51F32(nil, smpteF32)
	if err != nil {
		t.Fatalf("ConvertSMPTEToVorbis51F32: %v", err)
	}
	expectedF32 := []float32{1.0, 3.0, 2.0, 5.0, 6.0, 4.0}
	for i, v := range expectedF32 {
		if vorbisF32[i] != v {
			t.Errorf("channel %d: got %f, want %f", i, vorbisF32[i], v)
		}
	}
}


// A downmix that is "normalized" must have a total gain of 1 from the loudest left (or right) combination, so a
// half-scale signal in every channel comes out at half scale. Full scale cannot show this: an int16 or a clamped
// float saturates and hides a gain above 1, which is how 7.1 once shipped with a gain of 1.12.
func TestDownmixNormalizedGain(t *testing.T) {
	const half = 16384
	near := func(got, want int16, tol int16) bool { return got >= want-tol && got <= want+tol }

	in51 := []int16{half, half, half, half, half, half}
	out, err := Downmix51ToStereoNormalized(nil, in51)
	if err != nil {
		t.Fatal(err)
	}
	if !near(out[0], half, 2) || !near(out[1], half, 2) {
		t.Errorf("5.1 normalized: half scale in every channel gave %v, want about %d (total gain 1)", out, half)
	}

	in71 := []int16{half, half, half, half, half, half, half, half}
	out, err = Downmix71ToStereoNormalized(nil, in71)
	if err != nil {
		t.Fatal(err)
	}
	if !near(out[0], half, 2) || !near(out[1], half, 2) {
		t.Errorf("7.1 normalized: half scale in every channel gave %v, want about %d (total gain 1)", out, half)
	}

	f51, err := Downmix51ToStereoNormalizedF32(nil, []float32{0.5, 0.5, 0.5, 0.5, 0.5, 0.5})
	if err != nil {
		t.Fatal(err)
	}
	f71, err := Downmix71ToStereoNormalizedF32(nil, []float32{0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5})
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]float32{"5.1": f51, "7.1": f71} {
		for i, v := range got {
			if math.Abs(float64(v)-0.5) > 1e-4 {
				t.Errorf("%s float normalized: output[%d] = %v, want 0.5 (total gain 1)", name, i, v)
			}
		}
	}

	// Each channel's share on the left output, one at a time (7.1 order FL FC FR SL SR RL RR LFE): the shares of the
	// channels that feed L must add up to 1, and the LFE and the right-only channels must not leak in.
	var sum float64
	for ch, want := range map[int]float64{0: 1, 1: sqrt2Inv, 3: 0.5, 5: 0.5} {
		src := make([]float32, 8)
		src[ch] = 1
		o, _ := Downmix71ToStereoNormalizedF32(nil, src)
		sum += float64(o[0])
		if got, w := float64(o[0]), want*norm71; math.Abs(got-w) > 1e-5 {
			t.Errorf("7.1 channel %d feeds L with %v, want %v", ch, got, w)
		}
	}
	if math.Abs(sum-1) > 1e-4 {
		t.Errorf("7.1 left channels sum to %v, want 1", sum)
	}
	for _, ch := range []int{2, 4, 6, 7} {
		src := make([]float32, 8)
		src[ch] = 1
		if o, _ := Downmix71ToStereoNormalizedF32(nil, src); o[0] != 0 {
			t.Errorf("7.1 channel %d leaks %v into L", ch, o[0])
		}
	}
}
