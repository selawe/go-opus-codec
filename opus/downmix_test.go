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
