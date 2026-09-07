package opus

import (
	"math"
	"math/rand"
	"testing"

	"github.com/selawe/go-opus-codec/ogg"
)

// generateSineWave generates interleaved stereo 16-bit PCM for testing.
func generateSineWave(freq float64, sampleRate int, channels int, numSamples int) []int16 {
	pcm := make([]int16, numSamples*channels)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		val := int16(math.Sin(2*math.Pi*freq*t) * 28000)
		for c := 0; c < channels; c++ {
			pcm[i*channels+c] = val
		}
	}
	return pcm
}

func TestEncoderDecoderRoundtrip(t *testing.T) {
	const sampleRate = 48000
	const channels = 2
	const frameSize = 960 // 20ms at 48kHz

	enc, err := NewEncoder(sampleRate, channels, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	if err := enc.SetBitrate(64000); err != nil {
		t.Fatalf("SetBitrate: %v", err)
	}
	if err := enc.SetComplexity(10); err != nil {
		t.Fatalf("SetComplexity: %v", err)
	}

	dec, err := NewDecoder(sampleRate, channels)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	packetBuf := make([]byte, 1275)
	decodedBuf := make([]int16, 5760*channels)
	var allDecoded []int16
	const numFrames = 5

	for f := 0; f < numFrames; f++ {
		testPCM := generateSineWave(440, sampleRate, channels, frameSize)
		nB, err := enc.Encode(testPCM, frameSize, packetBuf)
		if err != nil {
			t.Fatalf("frame %d Encode error: %v", f, err)
		}
		dur, _ := PacketDuration(packetBuf[:nB])
		nD, err := dec.Decode(packetBuf[:nB], decodedBuf, 5760, false)
		if err != nil {
			t.Fatalf("frame %d Decode error: %v", f, err)
		}
		t.Logf("frame %d: nBytes=%d, TOC=0x%02X, dur=%v, nDecoded=%d", f, nB, packetBuf[0], dur, nD)
		allDecoded = append(allDecoded, decodedBuf[:nD*channels]...)
	}

	if len(allDecoded) == 0 {
		t.Fatal("expected decoded audio samples")
	}

	// Verify audio energy exists in decoded signal
	var decodedEnergy float64
	for i := range allDecoded {
		decodedEnergy += float64(allDecoded[i]) * float64(allDecoded[i])
	}
	if decodedEnergy <= 0 {
		t.Fatal("expected positive decoded audio energy")
	}
}

func TestDecoderPLC_RFC6716(t *testing.T) {
	// RFC 6716 Packet Loss Concealment test:
	// Decoding with a nil packet or empty slice indicates packet loss.
	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	pcm := make([]int16, 5760*2) // max 120ms buffer

	// First, simulate packet loss on a fresh decoder
	n, err := dec.Decode(nil, pcm, 960, false)
	if err != nil {
		t.Fatalf("PLC decode failed: %v", err)
	}
	if n != 960 {
		t.Fatalf("expected 960 PLC samples, got %d", n)
	}

	// DecodePacket with nil should also trigger PLC without panic
	out, nPerCh, err := dec.DecodePacket(nil, nil)
	if err != nil {
		t.Fatalf("DecodePacket(nil) failed: %v", err)
	}
	if nPerCh <= 0 || len(out) == 0 {
		t.Fatalf("expected non-empty PLC output, got nPerCh=%d, len=%d", nPerCh, len(out))
	}
}

func TestDecoderFloat32(t *testing.T) {
	enc, err := NewEncoder(48000, 1, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewDecoder(48000, 1)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	input := generateSineWave(1000, 48000, 1, 960)
	pkt := make([]byte, 1000)
	nBytes, err := enc.Encode(input, 960, pkt)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	pcmF32 := make([]float32, 5760)
	n, err := dec.DecodeF32(pkt[:nBytes], pcmF32, 5760, false)
	if err != nil {
		t.Fatalf("DecodeF32: %v", err)
	}
	if n <= 0 {
		t.Fatalf("DecodeF32 sample count: got %d", n)
	}

	// Verify all samples are bounded in normalized float range (allowing small filter ringing/overshoot)
	for i := 0; i < n; i++ {
		s := pcmF32[i]
		if s < -1.2 || s > 1.2 {
			t.Fatalf("sample %d out of float range: %v", i, s)
		}
	}
}

func TestDecoderResetAndGain(t *testing.T) {
	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	if err := dec.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// Set output gain +2 dB (512 Q7.8)
	if err := dec.SetGain(512); err != nil {
		t.Fatalf("SetGain: %v", err)
	}
}

func TestEncoderControls(t *testing.T) {
	enc, err := NewEncoder(48000, 2, ApplicationVoIP)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	if err := enc.SetBitrate(24000); err != nil {
		t.Fatalf("SetBitrate: %v", err)
	}
	if err := enc.SetVBR(false); err != nil {
		t.Fatalf("SetVBR(false): %v", err)
	}
	if err := enc.SetComplexity(5); err != nil {
		t.Fatalf("SetComplexity(5): %v", err)
	}
	if err := enc.SetDTX(true); err != nil {
		t.Fatalf("SetDTX(true): %v", err)
	}
	if err := enc.SetInbandFEC(true); err != nil {
		t.Fatalf("SetInbandFEC(true): %v", err)
	}
	if err := enc.SetPacketLossPerc(10); err != nil {
		t.Fatalf("SetPacketLossPerc(10): %v", err)
	}
	if err := enc.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	lookahead, err := enc.Lookahead()
	if err != nil {
		t.Fatalf("Lookahead: %v", err)
	}
	if lookahead <= 0 {
		t.Fatalf("expected positive lookahead, got %d", lookahead)
	}
}

func TestDecoderRobustnessFuzz(t *testing.T) {
	// RFC 6716 Section 4.5: The decoder MUST be robust against arbitrary bit errors.
	// It should reject or handle them gracefully without crashing or panicking.
	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	pcm := make([]int16, 5760*2)
	r := rand.New(rand.NewSource(42))

	for iter := 0; iter < 50; iter++ {
		// Generate random malformed packet lengths
		sz := r.Intn(500) + 1
		junk := make([]byte, sz)
		r.Read(junk)

		// Decoding junk should either succeed (producing concealment/noise) or return error,
		// but MUST NEVER panic or crash.
		_, _ = dec.Decode(junk, pcm, 960, false)
	}
}

func TestNewDecoderFromHead_OutputGain(t *testing.T) {
	enc, err := NewEncoder(48000, 1, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	pcm := generateSineWave(440, 48000, 1, 960)
	packet := make([]byte, 1000)
	nEnc, err := enc.Encode(pcm, 960, packet)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	headNoGain := ogg.OpusHead{
		Version:              1,
		Channels:             1,
		PreSkip:              312,
		InputSampleRate:      48000,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	decNoGain, err := NewDecoderFromHead(headNoGain)
	if err != nil {
		t.Fatalf("NewDecoderFromHead(headNoGain): %v", err)
	}
	defer decNoGain.Close()

	headWithGain := ogg.OpusHead{
		Version:              1,
		Channels:             1,
		PreSkip:              312,
		InputSampleRate:      48000,
		OutputGainQ8:         768, // +3.0 dB gain (~1.41x amplitude)
		ChannelMappingFamily: 0,
	}
	decWithGain, err := NewDecoderFromHead(headWithGain)
	if err != nil {
		t.Fatalf("NewDecoderFromHead(headWithGain): %v", err)
	}
	defer decWithGain.Close()

	outNoGain := make([]int16, 960)
	_, err = decNoGain.Decode(packet[:nEnc], outNoGain, 960, false)
	if err != nil {
		t.Fatalf("Decode no gain: %v", err)
	}

	outWithGain := make([]int16, 960)
	_, err = decWithGain.Decode(packet[:nEnc], outWithGain, 960, false)
	if err != nil {
		t.Fatalf("Decode with gain: %v", err)
	}

	var energyNoGain, energyWithGain float64
	for i := 100; i < 800; i++ {
		energyNoGain += float64(outNoGain[i]) * float64(outNoGain[i])
		energyWithGain += float64(outWithGain[i]) * float64(outWithGain[i])
	}

	if energyWithGain <= energyNoGain {
		t.Fatalf("expected higher energy with +3dB gain: withGain=%f, noGain=%f", energyWithGain, energyNoGain)
	}
}

