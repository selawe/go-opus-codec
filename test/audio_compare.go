package test

import (
	"fmt"
	"math"
	"os/exec"
	"strconv"
)

// AudioQualityResult holds comparative metrics between reference audio and decoded audio.
type AudioQualityResult struct {
	Correlation float64 // Pearson cross-correlation in [-1.0, 1.0]
	SNRdB       float64 // Signal-to-Noise Ratio in dB
	RMSE        float64 // Root Mean Square Error
	Samples     int     // Number of samples compared
}

// ComputeCorrelation calculates the Pearson cross-correlation coefficient between two int16 audio slices.
// Returns a value close to 1.0 for highly correlated signals.
func ComputeCorrelation(ref, dec []int16) float64 {
	n := min(len(ref), len(dec))
	if n == 0 {
		return 0
	}

	var sumRef, sumDec float64
	for i := 0; i < n; i++ {
		sumRef += float64(ref[i])
		sumDec += float64(dec[i])
	}
	meanRef := sumRef / float64(n)
	meanDec := sumDec / float64(n)

	var num, denRef, denDec float64
	for i := 0; i < n; i++ {
		dRef := float64(ref[i]) - meanRef
		dDec := float64(dec[i]) - meanDec
		num += dRef * dDec
		denRef += dRef * dRef
		denDec += dDec * dDec
	}

	den := math.Sqrt(denRef * denDec)
	if den == 0 {
		if denRef == 0 && denDec == 0 {
			return 1.0 // Both silent
		}
		return 0.0
	}
	return num / den
}

// ComputeSNR computes the Signal-to-Noise Ratio in decibels (dB) between reference and decoded audio.
func ComputeSNR(ref, dec []int16) float64 {
	n := min(len(ref), len(dec))
	if n == 0 {
		return 0
	}

	var signalPower, noisePower float64
	for i := 0; i < n; i++ {
		r := float64(ref[i])
		diff := r - float64(dec[i])
		signalPower += r * r
		noisePower += diff * diff
	}

	if noisePower == 0 {
		return 120.0 // Bit-exact or zero error
	}
	if signalPower == 0 {
		return 0.0
	}
	return 10.0 * math.Log10(signalPower/noisePower)
}

// ComputeRMSE computes the Root Mean Square Error between reference and decoded audio.
func ComputeRMSE(ref, dec []int16) float64 {
	n := min(len(ref), len(dec))
	if n == 0 {
		return 0
	}

	var sumSq float64
	for i := 0; i < n; i++ {
		diff := float64(ref[i]) - float64(dec[i])
		sumSq += diff * diff
	}
	return math.Sqrt(sumSq / float64(n))
}

// CompareAudio evaluates correlation, SNR, and RMSE between reference and decoded audio buffers.
func CompareAudio(ref, dec []int16) AudioQualityResult {
	n := min(len(ref), len(dec))
	return AudioQualityResult{
		Correlation: ComputeCorrelation(ref, dec),
		SNRdB:       ComputeSNR(ref, dec),
		RMSE:        ComputeRMSE(ref, dec),
		Samples:     n,
	}
}

// RunOpusCompare invokes the official libopus opus_compare tool if available on the system.
func RunOpusCompare(opusCompareBin string, rate, channels int, refFile, decFile string) (string, error) {
	args := []string{"-r", strconv.Itoa(rate), refFile, decFile}
	if channels == 2 {
		args = append([]string{"-s"}, args...)
	}

	cmd := exec.Command(opusCompareBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("opus_compare error: %w\n%s", err, string(out))
	}
	return string(out), nil
}
