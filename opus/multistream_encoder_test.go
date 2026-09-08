package opus

import (
	"math"
	"testing"

	"github.com/selawe/go-opus-codec/ogg"
)

func TestMultistreamEncoder_5_1_Surround_Roundtrip(t *testing.T) {
	const sampleRate = 48000
	const channels = 6
	const streams = 4
	const coupledStreams = 2
	const frameSize = 960

	// Standard Vorbis 5.1 channel mapping: FL, C, FR, RL, RR, LFE
	mapping := []uint8{0, 4, 1, 2, 3, 5}

	enc, err := NewMultistreamEncoder(sampleRate, channels, streams, coupledStreams, mapping, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewMultistreamEncoder: %v", err)
	}
	defer enc.Close()

	if enc.Channels() != channels {
		t.Errorf("expected %d channels, got %d", channels, enc.Channels())
	}
	if !enc.IsMultistream() {
		t.Error("expected IsMultistream to be true")
	}

	lookahead, err := enc.Lookahead()
	if err != nil || lookahead <= 0 {
		t.Errorf("expected positive lookahead, got %d (err: %v)", lookahead, err)
	}

	if err := enc.SetBitrate(192000); err != nil {
		t.Errorf("SetBitrate: %v", err)
	}
	if err := enc.SetVBR(true); err != nil {
		t.Errorf("SetVBR: %v", err)
	}
	if err := enc.SetComplexity(8); err != nil {
		t.Errorf("SetComplexity: %v", err)
	}

	dec, err := NewMultistreamDecoder(sampleRate, channels, streams, coupledStreams, mapping)
	if err != nil {
		t.Fatalf("NewMultistreamDecoder: %v", err)
	}
	defer dec.Close()

	if dec.Channels() != channels {
		t.Errorf("expected %d decoder channels, got %d", channels, dec.Channels())
	}
	if !dec.IsMultistream() {
		t.Error("expected decoder IsMultistream to be true")
	}

	// Generate 6-channel synthetic audio
	pcmIn := make([]int16, frameSize*channels)
	for i := 0; i < frameSize; i++ {
		for ch := 0; ch < channels; ch++ {
			freq := float64(220 * (ch + 1))
			val := int16(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)) * 10000)
			pcmIn[i*channels+ch] = val
		}
	}

	packet := make([]byte, 4000)
	nBytes, err := enc.Encode(pcmIn, frameSize, packet)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if nBytes <= 0 {
		t.Fatalf("expected encoded bytes > 0, got %d", nBytes)
	}

	rng, err := enc.FinalRange()
	if err != nil || rng == 0 {
		t.Errorf("expected non-zero FinalRange, got %x (err: %v)", rng, err)
	}

	pcmOut := make([]int16, frameSize*channels)
	nDecoded, err := dec.Decode(packet[:nBytes], pcmOut, frameSize, false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if nDecoded != frameSize {
		t.Errorf("expected %d decoded samples per channel, got %d", frameSize, nDecoded)
	}
}

func TestMultistreamEncoder_Float32_Roundtrip(t *testing.T) {
	const sampleRate = 48000
	const channels = 4 // Quadraphonic: FL, FR, RL, RR
	const streams = 2
	const coupledStreams = 2
	const frameSize = 960
	mapping := []uint8{0, 1, 2, 3}

	enc, err := NewMultistreamEncoder(sampleRate, channels, streams, coupledStreams, mapping, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewMultistreamEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewMultistreamDecoder(sampleRate, channels, streams, coupledStreams, mapping)
	if err != nil {
		t.Fatalf("NewMultistreamDecoder: %v", err)
	}
	defer dec.Close()

	pcmIn := make([]float32, frameSize*channels)
	for i := 0; i < frameSize; i++ {
		for ch := 0; ch < channels; ch++ {
			pcmIn[i*channels+ch] = float32(math.Sin(float64(i)*0.05)) * 0.5
		}
	}

	packet := make([]byte, 4000)
	nBytes, err := enc.EncodeF32(pcmIn, frameSize, packet)
	if err != nil {
		t.Fatalf("EncodeF32: %v", err)
	}

	pcmOut := make([]float32, frameSize*channels)
	nDecoded, err := dec.DecodeF32(packet[:nBytes], pcmOut, frameSize, false)
	if err != nil {
		t.Fatalf("DecodeF32: %v", err)
	}
	if nDecoded != frameSize {
		t.Errorf("expected %d decoded frames, got %d", frameSize, nDecoded)
	}
}

func TestNewEncoderFromHead_Multistream(t *testing.T) {
	head := ogg.OpusHead{
		Version:              1,
		Channels:             6,
		PreSkip:              312,
		InputSampleRate:      48000,
		OutputGainQ8:         0,
		ChannelMappingFamily: 1,
		StreamCount:          4,
		CoupledStreamCount:   2,
		ChannelMapping:       []uint8{0, 4, 1, 2, 3, 5},
	}

	enc, err := NewEncoderFromHead(head, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoderFromHead: %v", err)
	}
	defer enc.Close()

	if !enc.IsMultistream() {
		t.Error("expected IsMultistream to be true for Family 1 head")
	}
	if enc.Channels() != 6 {
		t.Errorf("expected 6 channels, got %d", enc.Channels())
	}

	dec, err := NewDecoderFromHead(head)
	if err != nil {
		t.Fatalf("NewDecoderFromHead: %v", err)
	}
	defer dec.Close()

	if !dec.IsMultistream() {
		t.Error("expected decoder IsMultistream to be true for Family 1 head")
	}

	pcmIn := make([]int16, 960*6)
	packet := make([]byte, 2000)
	nBytes, err := enc.Encode(pcmIn, 960, packet)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	pcmOut := make([]int16, 960*6)
	nDecoded, err := dec.Decode(packet[:nBytes], pcmOut, 960, false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if nDecoded != 960 {
		t.Errorf("expected 960 decoded samples, got %d", nDecoded)
	}
}

func TestMultistreamEncoder_ValidationErrors(t *testing.T) {
	mapping := []uint8{0, 1}

	// Invalid channels
	if _, err := NewMultistreamEncoder(48000, 0, 1, 0, mapping, ApplicationAudio); err == nil {
		t.Error("expected error for 0 channels")
	}

	// Invalid streams
	if _, err := NewMultistreamEncoder(48000, 2, 0, 0, mapping, ApplicationAudio); err == nil {
		t.Error("expected error for 0 streams")
	}

	// Coupled streams > streams
	if _, err := NewMultistreamEncoder(48000, 2, 1, 2, mapping, ApplicationAudio); err == nil {
		t.Error("expected error for coupled > streams")
	}

	// streams + coupled > channels
	if _, err := NewMultistreamEncoder(48000, 2, 2, 1, mapping, ApplicationAudio); err == nil {
		t.Error("expected error for streams + coupled > channels")
	}

	// Mapping length mismatch
	badMapping := []uint8{0}
	if _, err := NewMultistreamEncoder(48000, 2, 1, 1, badMapping, ApplicationAudio); err == nil {
		t.Error("expected error for mapping length mismatch")
	}
}
