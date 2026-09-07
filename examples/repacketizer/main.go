package main

import (
	"fmt"
	"log"
	"math"

	"github.com/selawe/go-opus-codec/opus"
)

func main() {
	const (
		sampleRate   = 48000
		channels     = 2
		frameSize20ms = sampleRate / 50 // 960 samples per channel = 20ms
	)

	fmt.Println("================================================================")
	fmt.Println(" Opus Repacketizer Example (RTP / WebRTC Frame Bundling)")
	fmt.Println("================================================================")
	fmt.Println("Demonstrates merging multiple small Opus packets into a single")
	fmt.Println("multi-frame packet (and extracting sub-ranges) without re-encoding.")
	fmt.Println()

	// 1. Initialize an encoder to generate test Opus frames
	enc, err := opus.NewEncoder(sampleRate, channels, opus.ApplicationAudio)
	if err != nil {
		log.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	if err := enc.SetBitrate(64000); err != nil {
		log.Fatalf("SetBitrate failed: %v", err)
	}

	// 2. Generate 3 consecutive 20ms audio frames (e.g., 440 Hz tone)
	packets := make([][]byte, 3)
	for i := 0; i < 3; i++ {
		pcm := make([]int16, frameSize20ms*channels)
		for s := 0; s < frameSize20ms; s++ {
			t := float64(i*frameSize20ms+s) / float64(sampleRate)
			val := int16(math.Sin(2*math.Pi*440.0*t) * 16000)
			pcm[s*channels] = val     // Left channel
			pcm[s*channels+1] = val   // Right channel
		}

		pktBuf := make([]byte, 1275)
		n, err := enc.Encode(pcm, frameSize20ms, pktBuf)
		if err != nil {
			log.Fatalf("Encode frame %d failed: %v", i+1, err)
		}
		packets[i] = pktBuf[:n]
		fmt.Printf(" [Encoder] Generated 20ms Frame %d: %d bytes (TOC: 0x%02x)\n",
			i+1, n, packets[i][0])
	}
	fmt.Println()

	// 3. Initialize Repacketizer and bundle all 3 frames into 1 packet
	rp, err := opus.NewRepacketizer()
	if err != nil {
		log.Fatalf("Failed to create repacketizer: %v", err)
	}
	defer rp.Close()

	for i, pkt := range packets {
		if err := rp.Cat(pkt); err != nil {
			log.Fatalf("Repacketizer.Cat frame %d failed: %v", i+1, err)
		}
	}

	fmt.Printf(" [Repacketizer] Buffered frames count: %d\n", rp.Frames())

	// Produce merged 60ms packet
	mergedBuf := make([]byte, 4000)
	mergedLen, err := rp.Out(mergedBuf)
	if err != nil {
		log.Fatalf("Repacketizer.Out failed: %v", err)
	}
	mergedPacket := mergedBuf[:mergedLen]

	framesInMerged, _ := opus.PacketFrameCount(mergedPacket)
	samplesPerFrame := opus.PacketSamplesPerFrame(mergedPacket, sampleRate)
	totalSamples, _ := opus.PacketTotalSamples(mergedPacket, sampleRate)

	fmt.Printf(" [Repacketizer] Merged Packet Size : %d bytes\n", mergedLen)
	fmt.Printf(" [Repacketizer] Frames in Packet   : %d\n", framesInMerged)
	fmt.Printf(" [Repacketizer] Samples per Frame  : %d (%d ms)\n",
		samplesPerFrame, samplesPerFrame*1000/sampleRate)
	fmt.Printf(" [Repacketizer] Total Audio Duration: %d samples (%d ms)\n\n",
		totalSamples, totalSamples*1000/sampleRate)

	// 4. Extract a sub-range of frames (e.g. frame index 1 to 2) using OutRange
	subBuf := make([]byte, 4000)
	subLen, err := rp.OutRange(1, 3, subBuf)
	if err != nil {
		log.Fatalf("Repacketizer.OutRange failed: %v", err)
	}
	subPacket := subBuf[:subLen]
	subFrames, _ := opus.PacketFrameCount(subPacket)
	fmt.Printf(" [Repacketizer] OutRange(1, 3) Extracted: %d bytes (%d frames)\n\n",
		subLen, subFrames)

	// 5. Decode the merged 60ms packet to verify audio validity
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		log.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Close()

	outPCM := make([]int16, totalSamples*channels)
	samplesDecoded, err := dec.Decode(mergedPacket, outPCM, totalSamples, false)
	if err != nil {
		log.Fatalf("Failed to decode merged packet: %v", err)
	}

	fmt.Printf(" [Decoder] Successfully decoded merged packet!\n")
	fmt.Printf("           Decoded: %d samples per channel (%d total PCM samples)\n",
		samplesDecoded, samplesDecoded*channels)
	fmt.Println("================================================================")
	fmt.Println(" Repacketizer demonstration completed successfully.")
	fmt.Println("================================================================")
}
