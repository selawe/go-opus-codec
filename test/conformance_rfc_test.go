package test

import (
	"bytes"
	"io"
	"math"
	"testing"

	"github.com/kazzmir/opus-go/ogg"
	"github.com/kazzmir/opus-go/opus"
	"github.com/kazzmir/opus-go/player"
)

// generateStereoTones generates a test audio signal with dual frequencies (440Hz and 880Hz).
func generateStereoTones(sampleRate int, durationSamples int) []int16 {
	pcm := make([]int16, durationSamples*2)
	for i := 0; i < durationSamples; i++ {
		t := float64(i) / float64(sampleRate)
		left := int16(math.Sin(2*math.Pi*440*t) * 20000)
		right := int16(math.Sin(2*math.Pi*880*t) * 20000)
		pcm[i*2] = left
		pcm[i*2+1] = right
	}
	return pcm
}

func TestRFC_FullPipelineConformance(t *testing.T) {
	const sampleRate = 48000
	const channels = 2
	const frameSize = 960 // 20ms at 48kHz
	const numFrames = 50  // 1.0 second of audio
	const totalAudioSamples = frameSize * numFrames

	inputPCM := generateStereoTones(sampleRate, totalAudioSamples)

	// 1. Setup Opus Encoder (RFC 6716)
	enc, err := opus.NewEncoder(sampleRate, channels, opus.ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	if err := enc.SetBitrate(96000); err != nil {
		t.Fatalf("SetBitrate: %v", err)
	}
	if err := enc.SetComplexity(10); err != nil {
		t.Fatalf("SetComplexity: %v", err)
	}

	lookahead, err := enc.Lookahead()
	if err != nil {
		t.Fatalf("Lookahead: %v", err)
	}

	// 2. Setup Ogg Bitstream & PacketWriter (RFC 3533 & RFC 7845)
	var oggBuf bytes.Buffer
	const bitstreamSerial uint32 = 0xABCD1234
	pw := ogg.NewPacketWriter(&oggBuf, bitstreamSerial)

	// Build and write OpusHead
	head := ogg.OpusHead{
		Version:              1,
		Channels:             uint8(channels),
		PreSkip:              uint16(lookahead),
		InputSampleRate:      sampleRate,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	headPkt, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		t.Fatalf("BuildOpusHeadPacket: %v", err)
	}
	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		t.Fatalf("WritePacket OpusHead: %v", err)
	}

	// Build and write OpusTags
	tags := ogg.OpusTags{
		Vendor: "opus-go-conformance",
		Comments: []string{
			"TITLE=RFC Conformance Test",
			"ARTIST=Automated Test Suite",
			"COMMENT=Verifying full RFC 6716 and RFC 7845 pipeline",
		},
	}
	tagsPkt, err := ogg.BuildOpusTagsPacket(tags)
	if err != nil {
		t.Fatalf("BuildOpusTagsPacket: %v", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		t.Fatalf("WritePacket OpusTags: %v", err)
	}

	// Encode and write all audio frames
	// To ensure all input samples are output by the encoder, feed one additional frame of padding
	// to flush the lookahead delay line. The final granule position is set to head.PreSkip + totalAudioSamples.
	totalFrames := numFrames + 1
	flushPCM := make([]int16, frameSize*channels)
	packetBuf := make([]byte, 1275)

	for f := 0; f < totalFrames; f++ {
		var framePCM []int16
		if f < numFrames {
			framePCM = inputPCM[f*frameSize*channels : (f+1)*frameSize*channels]
		} else {
			framePCM = flushPCM // padding to flush encoder lookahead
		}

		nBytes, err := enc.Encode(framePCM, frameSize, packetBuf)
		if err != nil {
			t.Fatalf("Encode frame %d: %v", f, err)
		}

		isLast := f == totalFrames-1
		granule := uint64(head.PreSkip) + uint64((f+1)*frameSize)
		if isLast {
			// RFC 7845 Section 4: The final granule indicates exact audio duration
			granule = uint64(head.PreSkip) + uint64(totalAudioSamples)
		}

		if err := pw.WritePacket(packetBuf[:nBytes], granule, false, isLast); err != nil {
			t.Fatalf("WritePacket audio frame %d: %v", f, err)
		}
	}

	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush Ogg writer: %v", err)
	}

	// 3. Read Ogg stream with OpusReader (RFC 7845 Conformance)
	streamBytes := oggBuf.Bytes()
	reader, err := ogg.NewOpusReader(bytes.NewReader(streamBytes))
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}

	if reader.Head.Channels != uint8(channels) {
		t.Fatalf("OpusHead Channels: got %d, want %d", reader.Head.Channels, channels)
	}
	if reader.Head.PreSkip != uint16(lookahead) {
		t.Fatalf("OpusHead PreSkip: got %d, want %d", reader.Head.PreSkip, lookahead)
	}
	if reader.Tags.Get("TITLE") != "RFC Conformance Test" {
		t.Fatalf("OpusTags TITLE: got %q", reader.Tags.Get("TITLE"))
	}
	if reader.Tags.Get("ARTIST") != "Automated Test Suite" {
		t.Fatalf("OpusTags ARTIST: got %q", reader.Tags.Get("ARTIST"))
	}

	totalSamples, err := reader.TotalSamples()
	if err != nil {
		t.Fatalf("reader.TotalSamples: %v", err)
	}
	if totalSamples != int64(totalAudioSamples) {
		t.Fatalf("total samples derived from granule mismatch: got %d, want %d", totalSamples, totalAudioSamples)
	}

	// 4. Decode full stream and verify Pre-skip and sample count
	dec, err := opus.NewDecoderFromHead(reader.Head)
	if err != nil {
		t.Fatalf("NewDecoderFromHead: %v", err)
	}
	defer dec.Close()

	reader2, err := ogg.NewOpusReader(bytes.NewReader(streamBytes))
	if err != nil {
		t.Fatalf("NewOpusReader pass 2: %v", err)
	}

	pcmDec := make([]int16, 5760*channels)
	preSkipRemaining := int(reader2.Head.PreSkip)
	expectedTotalInt16 := totalAudioSamples * channels
	var decodedAudio []int16

	for {
		pkt, err := reader2.ReadAudioPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadAudioPacket: %v", err)
		}

		nDecoded, err := dec.Decode(pkt.Data, pcmDec, 5760, false)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}

		out := pcmDec[:nDecoded*channels]

		// RFC 7845 Section 4: Discard pre-skip samples from beginning of stream
		if preSkipRemaining > 0 {
			skip := min(len(out), preSkipRemaining*channels)
			out = out[skip:]
			preSkipRemaining -= skip / channels
		}

		// RFC 7845 Section 4: Trim excess trailing samples past total audio duration
		if len(decodedAudio)+len(out) > expectedTotalInt16 {
			keep := expectedTotalInt16 - len(decodedAudio)
			out = out[:keep]
		}

		decodedAudio = append(decodedAudio, out...)

		if pkt.EOS {
			break
		}
	}

	// 5. Verify Exact Conformance of Decoded Output
	if len(decodedAudio) != expectedTotalInt16 {
		t.Fatalf("decoded int16 count mismatch: got %d, want %d", len(decodedAudio), expectedTotalInt16)
	}

	// Verify audio fidelity: compute normalized correlation between input and decoded signal
	// (accounting for codec lookahead / algorithmic delay)
	var dotProduct, inputNorm, decodedNorm float64
	for i := range decodedAudio {
		in := float64(inputPCM[i])
		out := float64(decodedAudio[i])
		dotProduct += in * out
		inputNorm += in * in
		decodedNorm += out * out
	}

	correlation := dotProduct / (math.Sqrt(inputNorm) * math.Sqrt(decodedNorm))
	t.Logf("Audio Correlation: %.4f (dot=%.0f, inNorm=%.0f, outNorm=%.0f)", correlation, dotProduct, inputNorm, decodedNorm)
	if correlation < 0.80 {
		t.Fatalf("audio fidelity too low: correlation = %.4f (want >= 0.80)", correlation)
	}

	// 6. Verify OpusPlayer High-Level API (opusgo)
	p, err := player.NewPlayerFromReader(bytes.NewReader(streamBytes))
	if err != nil {
		t.Fatalf("NewPlayerFromReader: %v", err)
	}

	playerData, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("ReadAll player: %v", err)
	}

	expectedByteLen := totalAudioSamples * channels * 2 // 16-bit PCM
	if len(playerData) != expectedByteLen {
		t.Fatalf("player total decoded bytes mismatch: got %d, want %d", len(playerData), expectedByteLen)
	}
}
