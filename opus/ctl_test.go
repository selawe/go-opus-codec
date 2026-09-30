package opus

import (
	"testing"
)

func TestEncoderCTLGettersAndSetters(t *testing.T) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder failed: %v", err)
	}
	defer enc.Close()

	// 1. Bitrate
	if err := enc.SetBitrate(96000); err != nil {
		t.Fatalf("SetBitrate failed: %v", err)
	}
	br, err := enc.Bitrate()
	if err != nil {
		t.Fatalf("Bitrate() failed: %v", err)
	}
	if br != 96000 {
		t.Errorf("expected bitrate 96000, got %d", br)
	}

	// 2. VBR
	if err := enc.SetVBR(false); err != nil {
		t.Fatalf("SetVBR(false) failed: %v", err)
	}
	vbr, err := enc.VBR()
	if err != nil || vbr != false {
		t.Errorf("expected VBR false, got %v (err: %v)", vbr, err)
	}
	if err := enc.SetVBR(true); err != nil {
		t.Fatalf("SetVBR(true) failed: %v", err)
	}
	vbr, err = enc.VBR()
	if err != nil || vbr != true {
		t.Errorf("expected VBR true, got %v (err: %v)", vbr, err)
	}

	// 3. VBRConstraint
	if err := enc.SetVBRConstraint(true); err != nil {
		t.Fatalf("SetVBRConstraint(true) failed: %v", err)
	}
	cbr, err := enc.VBRConstraint()
	if err != nil || cbr != true {
		t.Errorf("expected VBRConstraint true, got %v (err: %v)", cbr, err)
	}
	if err := enc.SetVBRConstraint(false); err != nil {
		t.Fatalf("SetVBRConstraint(false) failed: %v", err)
	}
	cbr, err = enc.VBRConstraint()
	if err != nil || cbr != false {
		t.Errorf("expected VBRConstraint false, got %v (err: %v)", cbr, err)
	}

	// 4. Complexity
	for _, comp := range []int{0, 5, 10} {
		if err := enc.SetComplexity(comp); err != nil {
			t.Fatalf("SetComplexity(%d) failed: %v", comp, err)
		}
		got, err := enc.Complexity()
		if err != nil || got != comp {
			t.Errorf("expected complexity %d, got %d (err: %v)", comp, got, err)
		}
	}

	// 5. DTX
	if err := enc.SetDTX(true); err != nil {
		t.Fatalf("SetDTX(true) failed: %v", err)
	}
	dtx, err := enc.DTX()
	if err != nil || dtx != true {
		t.Errorf("expected DTX true, got %v (err: %v)", dtx, err)
	}
	if err := enc.SetDTX(false); err != nil {
		t.Fatalf("SetDTX(false) failed: %v", err)
	}
	dtx, err = enc.DTX()
	if err != nil || dtx != false {
		t.Errorf("expected DTX false, got %v (err: %v)", dtx, err)
	}

	// 6. InbandFEC
	if err := enc.SetInbandFEC(true); err != nil {
		t.Fatalf("SetInbandFEC(true) failed: %v", err)
	}
	fec, err := enc.InbandFEC()
	if err != nil || fec != true {
		t.Errorf("expected InbandFEC true, got %v (err: %v)", fec, err)
	}
	if err := enc.SetInbandFEC(false); err != nil {
		t.Fatalf("SetInbandFEC(false) failed: %v", err)
	}
	fec, err = enc.InbandFEC()
	if err != nil || fec != false {
		t.Errorf("expected InbandFEC false, got %v (err: %v)", fec, err)
	}

	// 7. PacketLossPerc
	for _, loss := range []int{0, 15, 100} {
		if err := enc.SetPacketLossPerc(loss); err != nil {
			t.Fatalf("SetPacketLossPerc(%d) failed: %v", loss, err)
		}
		got, err := enc.PacketLossPerc()
		if err != nil || got != loss {
			t.Errorf("expected PacketLossPerc %d, got %d (err: %v)", loss, got, err)
		}
	}
	if err := enc.SetPacketLossPerc(-1); err == nil {
		t.Errorf("expected error for negative packet loss perc, got nil")
	}
	if err := enc.SetPacketLossPerc(101); err == nil {
		t.Errorf("expected error for >100 packet loss perc, got nil")
	}

	// 8. Bandwidth
	pcm := make([]int16, 960*2)
	pkt := make([]byte, 1275)
	for _, bw := range []Bandwidth{
		BandwidthNarrowband,
		BandwidthWideband,
		BandwidthSuperwideband,
		BandwidthFullband,
		BandwidthAuto,
	} {
		if err := enc.SetBandwidth(bw); err != nil {
			t.Fatalf("SetBandwidth(%s) failed: %v", bw, err)
		}
		if _, err := enc.Encode(pcm, 960, pkt); err != nil {
			t.Fatalf("Encode failed: %v", err)
		}
		got, err := enc.Bandwidth()
		if err != nil {
			t.Fatalf("Bandwidth() failed: %v", err)
		}
		if bw != BandwidthAuto && got != bw {
			t.Errorf("expected bandwidth %v, got %v", bw, got)
		}
	}
	// In ApplicationAudio (CELT mode), Mediumband is promoted to Wideband per libopus spec.
	if err := enc.SetBandwidth(BandwidthMediumband); err != nil {
		t.Fatalf("SetBandwidth(Mediumband) failed: %v", err)
	}
	if _, err := enc.Encode(pcm, 960, pkt); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if got, err := enc.Bandwidth(); err != nil || got != BandwidthWideband {
		t.Errorf("expected CELT Mediumband to promote to Wideband, got %v (err: %v)", got, err)
	}
	// In ApplicationVoIP (SILK mode), Mediumband is supported natively.
	voipEnc, err := NewEncoder(16000, 1, ApplicationVoIP)
	if err != nil {
		t.Fatalf("NewEncoder VoIP failed: %v", err)
	}
	defer voipEnc.Close()
	if err := voipEnc.SetBandwidth(BandwidthMediumband); err != nil {
		t.Fatalf("voip SetBandwidth(Mediumband) failed: %v", err)
	}
	voipPCM := make([]int16, 320)
	voipPkt := make([]byte, 1275)
	if _, err := voipEnc.Encode(voipPCM, 320, voipPkt); err != nil {
		t.Fatalf("voip Encode failed: %v", err)
	}
	if got, err := voipEnc.Bandwidth(); err != nil || got != BandwidthMediumband {
		t.Errorf("expected VoIP Mediumband to remain Mediumband, got %v (err: %v)", got, err)
	}
	if err := enc.SetBandwidth(Bandwidth(9999)); err == nil {
		t.Errorf("expected error for invalid bandwidth 9999, got nil")
	}

	// 9. MaxBandwidth
	for _, bw := range []Bandwidth{
		BandwidthNarrowband,
		BandwidthMediumband,
		BandwidthWideband,
		BandwidthSuperwideband,
		BandwidthFullband,
	} {
		if err := enc.SetMaxBandwidth(bw); err != nil {
			t.Fatalf("SetMaxBandwidth(%s) failed: %v", bw, err)
		}
		got, err := enc.MaxBandwidth()
		if err != nil || got != bw {
			t.Errorf("expected max bandwidth %v, got %v (err: %v)", bw, got, err)
		}
	}
	if err := enc.SetMaxBandwidth(BandwidthAuto); err == nil {
		t.Errorf("expected error for SetMaxBandwidth(BandwidthAuto), got nil")
	}
	if err := enc.SetMaxBandwidth(Bandwidth(9999)); err == nil {
		t.Errorf("expected error for invalid max bandwidth 9999, got nil")
	}

	// 10. Signal
	for _, sig := range []Signal{SignalAuto, SignalVoice, SignalMusic} {
		if err := enc.SetSignal(sig); err != nil {
			t.Fatalf("SetSignal(%s) failed: %v", sig, err)
		}
		got, err := enc.Signal()
		if err != nil || got != sig {
			t.Errorf("expected signal %v, got %v (err: %v)", sig, got, err)
		}
	}
	if err := enc.SetSignal(Signal(9999)); err == nil {
		t.Errorf("expected error for invalid signal 9999, got nil")
	}

	// 11. ForceChannels
	for _, ch := range []int{Auto, 1, 2} {
		if err := enc.SetForceChannels(ch); err != nil {
			t.Fatalf("SetForceChannels(%d) failed: %v", ch, err)
		}
		got, err := enc.ForceChannels()
		if err != nil || got != ch {
			t.Errorf("expected force channels %d, got %d (err: %v)", ch, got, err)
		}
	}
	if err := enc.SetForceChannels(3); err == nil {
		t.Errorf("expected error for forcing 3 channels on stereo encoder, got nil")
	}

	// 12. LSBDepth
	for _, depth := range []int{8, 16, 24} {
		if err := enc.SetLSBDepth(depth); err != nil {
			t.Fatalf("SetLSBDepth(%d) failed: %v", depth, err)
		}
		got, err := enc.LSBDepth()
		if err != nil || got != depth {
			t.Errorf("expected LSB depth %d, got %d (err: %v)", depth, got, err)
		}
	}
	if err := enc.SetLSBDepth(7); err == nil {
		t.Errorf("expected error for LSB depth 7, got nil")
	}
	if err := enc.SetLSBDepth(25); err == nil {
		t.Errorf("expected error for LSB depth 25, got nil")
	}

	// 13. PredictionDisabled
	if err := enc.SetPredictionDisabled(true); err != nil {
		t.Fatalf("SetPredictionDisabled(true) failed: %v", err)
	}
	pred, err := enc.PredictionDisabled()
	if err != nil || pred != true {
		t.Errorf("expected PredictionDisabled true, got %v (err: %v)", pred, err)
	}
	if err := enc.SetPredictionDisabled(false); err != nil {
		t.Fatalf("SetPredictionDisabled(false) failed: %v", err)
	}
	pred, err = enc.PredictionDisabled()
	if err != nil || pred != false {
		t.Errorf("expected PredictionDisabled false, got %v (err: %v)", pred, err)
	}

	// 14. PhaseInversionDisabled
	if err := enc.SetPhaseInversionDisabled(true); err != nil {
		t.Fatalf("SetPhaseInversionDisabled(true) failed: %v", err)
	}
	phase, err := enc.PhaseInversionDisabled()
	if err != nil || phase != true {
		t.Errorf("expected PhaseInversionDisabled true, got %v (err: %v)", phase, err)
	}
	if err := enc.SetPhaseInversionDisabled(false); err != nil {
		t.Fatalf("SetPhaseInversionDisabled(false) failed: %v", err)
	}
	phase, err = enc.PhaseInversionDisabled()
	if err != nil || phase != false {
		t.Errorf("expected PhaseInversionDisabled false, got %v (err: %v)", phase, err)
	}
}

func TestMultistreamEncoderCTL(t *testing.T) {
	// 5.1 surround sound: 6 channels, 4 streams, 2 coupled streams
	mapping := []uint8{0, 4, 1, 2, 3, 5}
	enc, err := NewMultistreamEncoder(48000, 6, 4, 2, mapping, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewMultistreamEncoder failed: %v", err)
	}
	defer enc.Close()

	if err := enc.SetSignal(SignalMusic); err != nil {
		t.Fatalf("SetSignal on multistream encoder failed: %v", err)
	}
	sig, err := enc.Signal()
	if err != nil || sig != SignalMusic {
		t.Errorf("expected SignalMusic, got %v (err: %v)", sig, err)
	}

	if err := enc.SetMaxBandwidth(BandwidthFullband); err != nil {
		t.Fatalf("SetMaxBandwidth on multistream encoder failed: %v", err)
	}
	bw, err := enc.MaxBandwidth()
	if err != nil || bw != BandwidthFullband {
		t.Errorf("expected BandwidthFullband, got %v (err: %v)", bw, err)
	}
}

func TestClosedEncoderCTLReturnsError(t *testing.T) {
	enc, err := NewEncoder(48000, 1, ApplicationVoIP)
	if err != nil {
		t.Fatalf("NewEncoder failed: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify all new methods return error on closed encoder
	if _, err := enc.Bitrate(); err == nil {
		t.Error("expected error from Bitrate on closed encoder")
	}
	if _, err := enc.VBR(); err == nil {
		t.Error("expected error from VBR on closed encoder")
	}
	if _, err := enc.VBRConstraint(); err == nil {
		t.Error("expected error from VBRConstraint on closed encoder")
	}
	if _, err := enc.Complexity(); err == nil {
		t.Error("expected error from Complexity on closed encoder")
	}
	if _, err := enc.DTX(); err == nil {
		t.Error("expected error from DTX on closed encoder")
	}
	if _, err := enc.InbandFEC(); err == nil {
		t.Error("expected error from InbandFEC on closed encoder")
	}
	if _, err := enc.PacketLossPerc(); err == nil {
		t.Error("expected error from PacketLossPerc on closed encoder")
	}
	if _, err := enc.Bandwidth(); err == nil {
		t.Error("expected error from Bandwidth on closed encoder")
	}
	if _, err := enc.MaxBandwidth(); err == nil {
		t.Error("expected error from MaxBandwidth on closed encoder")
	}
	if _, err := enc.Signal(); err == nil {
		t.Error("expected error from Signal on closed encoder")
	}
	if _, err := enc.ForceChannels(); err == nil {
		t.Error("expected error from ForceChannels on closed encoder")
	}
	if _, err := enc.LSBDepth(); err == nil {
		t.Error("expected error from LSBDepth on closed encoder")
	}
	if _, err := enc.PredictionDisabled(); err == nil {
		t.Error("expected error from PredictionDisabled on closed encoder")
	}
	if _, err := enc.PhaseInversionDisabled(); err == nil {
		t.Error("expected error from PhaseInversionDisabled on closed encoder")
	}
	if err := enc.SetSignal(SignalVoice); err == nil {
		t.Error("expected error from SetSignal on closed encoder")
	}
}

func TestBandwidthAndSignalStrings(t *testing.T) {
	tests := []struct {
		b    Bandwidth
		want string
	}{
		{BandwidthAuto, "Auto"},
		{BandwidthNarrowband, "Narrowband (8 kHz)"},
		{BandwidthMediumband, "Mediumband (12 kHz)"},
		{BandwidthWideband, "Wideband (16 kHz)"},
		{BandwidthSuperwideband, "Superwideband (24 kHz)"},
		{BandwidthFullband, "Fullband (48 kHz)"},
		{Bandwidth(999), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.b.String(); got != tt.want {
			t.Errorf("Bandwidth(%d).String() = %q, want %q", int(tt.b), got, tt.want)
		}
	}

	sigTests := []struct {
		s    Signal
		want string
	}{
		{SignalAuto, "auto"},
		{SignalVoice, "voice"},
		{SignalMusic, "music"},
		{Signal(999), "Signal(999)"},
	}
	for _, tt := range sigTests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Signal(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}
