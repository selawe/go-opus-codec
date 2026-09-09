package opus

import (
	"math"
	"runtime"
	"testing"
)

func TestRepacketizer_MergeAndDecode(t *testing.T) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	rp, err := NewRepacketizer()
	if err != nil {
		t.Fatalf("NewRepacketizer: %v", err)
	}
	defer rp.Close()

	// Generate 3 consecutive 20ms frames (960 samples per channel each)
	const frameSamples = 960
	const channels = 2
	var packets [][]byte

	for f := 0; f < 3; f++ {
		pcm := make([]int16, frameSamples*channels)
		for i := 0; i < frameSamples; i++ {
			sampleIndex := f*frameSamples + i
			val := int16(10000 * math.Sin(2*math.Pi*440*float64(sampleIndex)/48000))
			pcm[i*2] = val
			pcm[i*2+1] = val
		}
		pkt := make([]byte, 1000)
		n, err := enc.Encode(pcm, frameSamples, pkt)
		if err != nil {
			t.Fatalf("frame %d Encode: %v", f, err)
		}
		packets = append(packets, pkt[:n])
	}

	// Add packets to repacketizer
	for i, pkt := range packets {
		if err := rp.Cat(pkt); err != nil {
			t.Fatalf("Cat frame %d: %v", i, err)
		}
	}

	if got := rp.Frames(); got != 3 {
		t.Fatalf("expected 3 frames, got %d", got)
	}

	// Output merged packet
	merged := make([]byte, 3000)
	nMerged, err := rp.Out(merged)
	if err != nil {
		t.Fatalf("rp.Out: %v", err)
	}
	merged = merged[:nMerged]

	// Inspect merged packet using RFC 6716 inspection helpers
	dur, err := PacketDuration(merged)
	if err != nil {
		t.Fatalf("PacketDuration: %v", err)
	}
	if dur.Milliseconds() != 60 {
		t.Fatalf("expected 60ms duration, got %v", dur)
	}

	nbFrames, err := PacketFrameCount(merged)
	if err != nil {
		t.Fatalf("PacketFrameCount: %v", err)
	}
	if nbFrames != 3 {
		t.Fatalf("expected 3 frames, got %d", nbFrames)
	}

	demuxedFrames, err := PacketFrames(merged)
	if err != nil {
		t.Fatalf("PacketFrames: %v", err)
	}
	if len(demuxedFrames) != 3 {
		t.Fatalf("expected 3 demuxed frames, got %d", len(demuxedFrames))
	}

	// Decode the combined packet: should yield 3 * 960 = 2880 samples per channel
	outPCM := make([]int16, 2880*channels)
	samplesDecoded, err := dec.Decode(merged, outPCM, 2880, false)
	if err != nil {
		t.Fatalf("Decode merged packet: %v", err)
	}
	if samplesDecoded != 2880 {
		t.Fatalf("expected 2880 decoded samples per channel, got %d", samplesDecoded)
	}

	// Test OutRange: extract only frames [1, 3) (2 frames = 40ms)
	subPkt := make([]byte, 2000)
	nSub, err := rp.OutRange(1, 3, subPkt)
	if err != nil {
		t.Fatalf("OutRange: %v", err)
	}
	subDur, err := PacketDuration(subPkt[:nSub])
	if err != nil {
		t.Fatalf("sub-packet duration: %v", err)
	}
	if subDur.Milliseconds() != 40 {
		t.Fatalf("expected 40ms duration for 2 extracted frames, got %v", subDur)
	}

	// Test Reset
	if err := rp.Reset(); err != nil {
		t.Fatalf("rp.Reset: %v", err)
	}
	if got := rp.Frames(); got != 0 {
		t.Fatalf("expected 0 frames after Reset, got %d", got)
	}
}

func TestPacketPad_And_Unpad(t *testing.T) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	// Encode a simple frame
	pcm := make([]int16, 960*2)
	for i := range pcm {
		pcm[i] = int16(5000 * math.Sin(float64(i)*0.05))
	}
	pkt := make([]byte, 500)
	n, err := enc.Encode(pcm, 960, pkt)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	rawPacket := pkt[:n]

	// Pad to larger size
	targetLen := n + 64
	padded, err := PacketPad(rawPacket, targetLen)
	if err != nil {
		t.Fatalf("PacketPad: %v", err)
	}
	if len(padded) != targetLen {
		t.Fatalf("expected padded length %d, got %d", targetLen, len(padded))
	}

	// Decoding padded packet should work and produce exactly 960 samples
	outPCM := make([]int16, 960*2)
	nSamples, err := dec.Decode(padded, outPCM, 960, false)
	if err != nil {
		t.Fatalf("Decode padded: %v", err)
	}
	if nSamples != 960 {
		t.Fatalf("expected 960 decoded samples, got %d", nSamples)
	}

	// Unpad
	unpadded, err := PacketUnpad(padded)
	if err != nil {
		t.Fatalf("PacketUnpad: %v", err)
	}
	if len(unpadded) != len(rawPacket) {
		t.Fatalf("expected unpadded length %d, got %d", len(rawPacket), len(unpadded))
	}

	// Multistream pad and unpad (1 stream)
	msPadded, err := MultistreamPacketPad(rawPacket, targetLen, 1)
	if err != nil {
		t.Fatalf("MultistreamPacketPad: %v", err)
	}
	if len(msPadded) != targetLen {
		t.Fatalf("expected msPadded length %d, got %d", targetLen, len(msPadded))
	}

	msUnpadded, err := MultistreamPacketUnpad(msPadded, 1)
	if err != nil {
		t.Fatalf("MultistreamPacketUnpad: %v", err)
	}
	if len(msUnpadded) != len(rawPacket) {
		t.Fatalf("expected msUnpadded length %d, got %d", len(rawPacket), len(msUnpadded))
	}
}

func TestSoftClip(t *testing.T) {
	const channels = 2
	const count = 1000

	// Create samples exceeding [-1.0, 1.0]
	pcm := make([]float32, count*channels)
	for i := 0; i < count; i++ {
		val := float32(2.5 * math.Sin(float64(i)*0.1))
		pcm[i*channels] = val
		pcm[i*channels+1] = -val
	}

	if err := SoftClip(pcm, channels); err != nil {
		t.Fatalf("SoftClip: %v", err)
	}

	for i, s := range pcm {
		if s > 1.0 || s < -1.0 {
			t.Fatalf("sample %d = %f exceeds [-1.0, 1.0]", i, s)
		}
	}

	// Test stateful SoftClipper
	sc, err := NewSoftClipper(channels)
	if err != nil {
		t.Fatalf("NewSoftClipper: %v", err)
	}
	defer sc.Close()

	pcmChunk := make([]float32, 200*channels)
	for i := range pcmChunk {
		pcmChunk[i] = 1.8
	}
	if err := sc.Process(pcmChunk); err != nil {
		t.Fatalf("sc.Process: %v", err)
	}
	for i, s := range pcmChunk {
		if s > 1.0 || s < -1.0 {
			t.Fatalf("chunk sample %d = %f exceeds [-1.0, 1.0]", i, s)
		}
	}
	sc.Reset()
}

func TestFinalRange(t *testing.T) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	pcm := make([]int16, 960*2)
	for i := range pcm {
		pcm[i] = int16(8000 * math.Sin(float64(i)*0.02))
	}

	pkt := make([]byte, 1000)
	n, err := enc.Encode(pcm, 960, pkt)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	encRange, err := enc.FinalRange()
	if err != nil {
		t.Fatalf("enc.FinalRange: %v", err)
	}
	if encRange == 0 {
		t.Fatal("expected non-zero encoder final range")
	}

	outPCM := make([]int16, 960*2)
	_, err = dec.Decode(pkt[:n], outPCM, 960, false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	decRange, err := dec.FinalRange()
	if err != nil {
		t.Fatalf("dec.FinalRange: %v", err)
	}
	if decRange == 0 {
		t.Fatal("expected non-zero decoder final range")
	}

	// Note: in CELT or full-band modes, entropy range reflects bit-exact state.
	t.Logf("enc final range: 0x%08x, dec final range: 0x%08x", encRange, decRange)
}

func TestRepacketizer_NoUseAfterFree(t *testing.T) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	rp, err := NewRepacketizer()
	if err != nil {
		t.Fatalf("NewRepacketizer: %v", err)
	}
	defer rp.Close()

	pcm := make([]int16, 960*2)
	for i := range pcm {
		pcm[i] = int16(5000 * math.Sin(float64(i)*0.05))
	}

	// Function that creates an ephemeral heap-allocated slice and drops its reference
	catPacket := func(rp *Repacketizer) {
		pkt := make([]byte, 1000)
		n, err := enc.Encode(pcm, 960, pkt)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		// Allocate isolated heap slice and pass to Cat
		ephemeral := make([]byte, n)
		copy(ephemeral, pkt[:n])
		if err := rp.Cat(ephemeral); err != nil {
			t.Fatalf("Cat: %v", err)
		}
	}

	catPacket(rp)
	catPacket(rp)

	// Force Go runtime GC to reclaim unreachable ephemeral slices
	runtime.GC()

	// Poison the heap with dummy allocations
	for i := 0; i < 100; i++ {
		poison := make([]byte, 1024)
		for j := range poison {
			poison[j] = 0xAA
		}
		_ = poison
	}

	merged := make([]byte, 3000)
	nMerged, err := rp.Out(merged)
	if err != nil {
		t.Fatalf("rp.Out: %v", err)
	}

	outPCM := make([]int16, 960*2*2)
	nSamples, err := dec.Decode(merged[:nMerged], outPCM, 960*2, false)
	if err != nil {
		t.Fatalf("Decode merged packet failed (corrupted by UAF): %v", err)
	}
	if nSamples != 960*2 {
		t.Fatalf("expected %d samples, got %d", 960*2, nSamples)
	}
}

func TestRepacketizer_BufferReuse(t *testing.T) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	rp, err := NewRepacketizer()
	if err != nil {
		t.Fatalf("NewRepacketizer: %v", err)
	}
	defer rp.Close()

	pcm := make([]int16, 960*2)
	for i := range pcm {
		pcm[i] = int16(4000 * math.Sin(float64(i)*0.03))
	}
	reusableBuf := make([]byte, 1000)

	// Add first frame using reusableBuf
	n1, err := enc.Encode(pcm, 960, reusableBuf)
	if err != nil {
		t.Fatalf("Encode 1: %v", err)
	}
	if err := rp.Cat(reusableBuf[:n1]); err != nil {
		t.Fatalf("Cat 1: %v", err)
	}

	// Overwrite reusableBuf completely with 0xFF before second frame
	for i := range reusableBuf {
		reusableBuf[i] = 0xFF
	}

	// Add second frame
	n2, err := enc.Encode(pcm, 960, reusableBuf)
	if err != nil {
		t.Fatalf("Encode 2: %v", err)
	}
	if err := rp.Cat(reusableBuf[:n2]); err != nil {
		t.Fatalf("Cat 2: %v", err)
	}

	// Overwrite reusableBuf again with poison
	for i := range reusableBuf {
		reusableBuf[i] = 0xAA
	}

	merged := make([]byte, 3000)
	nMerged, err := rp.Out(merged)
	if err != nil {
		t.Fatalf("rp.Out: %v", err)
	}

	outPCM := make([]int16, 960*2*2)
	nSamples, err := dec.Decode(merged[:nMerged], outPCM, 960*2, false)
	if err != nil {
		t.Fatalf("Decode failed after buffer reuse: %v", err)
	}
	if nSamples != 960*2 {
		t.Fatalf("expected %d samples, got %d", 960*2, nSamples)
	}
}
