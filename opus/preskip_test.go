package opus

import "testing"

// Lookahead is in samples at the encoder's rate; OpusHead.PreSkip is always in 48 kHz samples.
func TestEncoderPreSkipIs48kHzUnits(t *testing.T) {
	for _, rate := range []int{8000, 12000, 16000, 24000, 48000} {
		enc, err := NewEncoder(rate, 1, ApplicationAudio)
		if err != nil {
			t.Fatalf("rate %d: %v", rate, err)
		}
		la, err := enc.Lookahead()
		if err != nil {
			t.Fatal(err)
		}
		ps, err := enc.PreSkip()
		if err != nil {
			t.Fatal(err)
		}
		if want := la * 48000 / rate; ps != want {
			t.Errorf("rate %d: PreSkip = %d, want lookahead %d scaled to 48 kHz = %d", rate, ps, la, want)
		}
		// libopus: Fs/400 + Fs/250 samples at the encoder rate, which is 312 at 48 kHz
		// and scales to the same 312 at every other rate.
		if ps != 312 {
			t.Errorf("rate %d: PreSkip = %d, want 312", rate, ps)
		}
		_ = enc.Close()
	}
	closed, _ := NewEncoder(48000, 1, ApplicationAudio)
	_ = closed.Close()
	if _, err := closed.PreSkip(); err == nil {
		t.Error("PreSkip on a closed encoder should fail")
	}
}
