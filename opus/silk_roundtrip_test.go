package opus

import (
	"testing"
)

// TestSILKRoundtrip guards the SILK/hybrid paths used by VoIP (e.g. WebRTC).
// A signed-to-unsigned conversion that zero-extended instead of sign-extending
// once made both the SILK encoder (bitrate several times the target) and the
// SILK decoder (output pinned at full scale) produce loud noise, while the
// CELT-only tests kept passing.
func TestSILKRoundtrip(t *testing.T) {
	const (
		rate      = 48000
		frameSize = 960
		frames    = 150
	)
	for _, bitrate := range []int{12000, 16000, 32000} {
		enc, err := NewEncoder(rate, 1, ApplicationVoIP)
		if err != nil {
			t.Fatalf("NewEncoder: %v", err)
		}
		if err := enc.SetBitrate(bitrate); err != nil {
			t.Fatalf("SetBitrate: %v", err)
		}
		dec, err := NewDecoder(rate, 1)
		if err != nil {
			t.Fatalf("NewDecoder: %v", err)
		}

		src := generateHarmonics(frames * frameSize)
		packet := make([]byte, 1500)
		pcm := make([]float32, 5760)
		var decoded []float32
		totalBytes := 0
		for f := range frames {
			n, err := enc.EncodeF32(src[f*frameSize:(f+1)*frameSize], frameSize, packet)
			if err != nil {
				t.Fatalf("bitrate %d frame %d: Encode: %v", bitrate, f, err)
			}
			totalBytes += n
			got, err := dec.DecodeF32(packet[:n], pcm, 5760, false)
			if err != nil {
				t.Fatalf("bitrate %d frame %d: Decode: %v", bitrate, f, err)
			}
			decoded = append(decoded, pcm[:got]...)
		}
		_ = enc.Close()
		_ = dec.Close()

		gotBitrate := totalBytes * 8 * rate / (frames * frameSize)
		if gotBitrate > bitrate*3/2 {
			t.Errorf("bitrate %d: encoder produced %d bps, want <= %d", bitrate, gotBitrate, bitrate*3/2)
		}
		if snr := computeSNR(src, decoded); snr < 3 {
			t.Errorf("bitrate %d: roundtrip SNR = %.2f dB, want >= 3 dB", bitrate, snr)
		}
	}
}
