package test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/selawe/go-opus-codec/opus"
)

// TestRFC6716_PCMOutput compares decoded PCM against the reference .dec files
// (48 kHz stereo). The final-range check in TestRFC6716_FullConformanceMatrix
// only covers the entropy decoder, so it cannot catch errors in synthesis
// (e.g. SILK output saturating to full scale).
//
// The SILK-only vectors 02-04 must match bit-exactly. The other vectors are
// held to a loose SNR floor: the .dec files predate the RFC 8251 decoder
// updates, which change hybrid output in particular.
func TestRFC6716_PCMOutput(t *testing.T) {
	vectorDir := os.Getenv("OPUS_RFC6716_TESTVECTORS")
	if vectorDir == "" {
		vectorDir = filepath.Join("..", "testvectors")
	}
	if _, err := os.Stat(filepath.Join(vectorDir, "testvector01.dec")); err != nil {
		const hint = "RFC 6716 test vectors not found. Set OPUS_RFC6716_TESTVECTORS or run scripts/download_testvectors.sh"
		if os.Getenv("OPUS_REQUIRE_TESTVECTORS") != "" || os.Getenv("CI") != "" {
			t.Fatal(hint)
		}
		t.Skip(hint)
	}

	const minSNRdB = 15
	bitExact := map[string]bool{"02": true, "03": true, "04": true}

	for v := 1; v <= 12; v++ {
		vec := fmt.Sprintf("%02d", v)
		t.Run("vec_"+vec, func(t *testing.T) {
			t.Parallel()

			got, err := decodeVectorPCM(filepath.Join(vectorDir, "testvector"+vec+".bit"), 48000, 2)
			if err != nil {
				t.Fatal(err)
			}
			refBytes, err := os.ReadFile(filepath.Join(vectorDir, "testvector"+vec+".dec"))
			if err != nil {
				t.Fatal(err)
			}
			if len(got)*2 != len(refBytes) {
				t.Fatalf("decoded %d samples, reference has %d", len(got), len(refBytes)/2)
			}

			var sigPower, errPower float64
			maxDiff := 0
			for i, s := range got {
				ref := int16(binary.LittleEndian.Uint16(refBytes[2*i:]))
				d := int(s) - int(ref)
				sigPower += float64(ref) * float64(ref)
				errPower += float64(d * d)
				if d < 0 {
					d = -d
				}
				maxDiff = max(maxDiff, d)
			}
			snr := 10 * math.Log10(sigPower/(errPower+1e-9))

			if bitExact[vec] && maxDiff != 0 {
				t.Errorf("want bit-exact output, max sample diff %d (SNR %.1f dB)", maxDiff, snr)
			}
			if snr < minSNRdB {
				t.Errorf("SNR vs reference = %.1f dB, want >= %d dB (max sample diff %d)", snr, minSNRdB, maxDiff)
			}
		})
	}
}

func decodeVectorPCM(bitPath string, rate, channels int) ([]int16, error) {
	f, err := os.Open(bitPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec, err := opus.NewDecoder(rate, channels)
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	maxFrameSamples := rate * 120 / 1000
	buf := make([]int16, maxFrameSamples*channels)
	reader := NewBitstreamReader(f)
	var out []int16
	for i := 0; ; i++ {
		frame, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("frame %d read: %w", i, err)
		}
		var payload []byte
		if !frame.IsPacketLoss {
			payload = frame.Payload
		}
		n, err := dec.Decode(payload, buf, maxFrameSamples, false)
		if err != nil {
			return nil, fmt.Errorf("frame %d decode: %w", i, err)
		}
		out = append(out, buf[:n*channels]...)
	}
}
