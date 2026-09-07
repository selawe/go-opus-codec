package main

import (
	"fmt"
	"log"
	"math"

	"github.com/selawe/go-opus-codec/opus"
)

func main() {
	const (
		sampleRate    = 48000
		channels      = 2
		frameSize20ms = 960 // 20ms @ 48kHz
		totalFrames   = 5
	)

	fmt.Println("================================================================")
	fmt.Println(" Opus Roundtrip PCM Example (Encode, Decode, SoftClip, & PLC)")
	fmt.Println("================================================================")
	fmt.Println("Demonstrates:")
	fmt.Println(" 1. Float32 SoftClip dynamic range compression on hot audio peaks")
	fmt.Println(" 2. Encoder configuration: Bitrate, VBR, Complexity, In-Band FEC")
	fmt.Println(" 3. Transmission simulation with intentional packet loss")
	fmt.Println(" 4. Decoder Packet Loss Concealment (PLC) on lost frames")
	fmt.Println(" 5. Entropy FinalRange inspection and audio quality metrics")
	fmt.Println()

	// 1. Synthesize audio with intentional hot peaks (> 1.0) on frame 1
	fmt.Println("--- Step 1: Float32 SoftClip Demo ---")
	hotPCM := make([]float32, frameSize20ms*channels)
	var maxPreClip float32
	for i := 0; i < frameSize20ms; i++ {
		t := float64(i) / float64(sampleRate)
		// Loud signal with peaks reaching 1.8 (> 1.0)
		val := float32(math.Sin(2*math.Pi*440.0*t) * 1.8)
		hotPCM[i*channels] = val
		hotPCM[i*channels+1] = val
		if float32(math.Abs(float64(val))) > maxPreClip {
			maxPreClip = float32(math.Abs(float64(val)))
		}
	}
	fmt.Printf(" [SoftClip] Peak amplitude before clipping: %.3f (exceeds 1.0!)\n", maxPreClip)

	if err := opus.SoftClip(hotPCM, channels); err != nil {
		log.Fatalf("SoftClip failed: %v", err)
	}

	var maxPostClip float32
	for _, v := range hotPCM {
		if float32(math.Abs(float64(v))) > maxPostClip {
			maxPostClip = float32(math.Abs(float64(v)))
		}
	}
	fmt.Printf(" [SoftClip] Peak amplitude after SoftClip  : %.3f (smoothly bounded in [-1.0, 1.0])\n\n", maxPostClip)

	// 2. Initialize and configure Opus Encoder
	fmt.Println("--- Step 2: Encoder Configuration & Encoding ---")
	enc, err := opus.NewEncoder(sampleRate, channels, opus.ApplicationAudio)
	if err != nil {
		log.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	const targetBitrate = 64000
	const targetComplexity = 10

	_ = enc.SetBitrate(targetBitrate)
	_ = enc.SetVBR(true)
	_ = enc.SetComplexity(targetComplexity)
	_ = enc.SetInbandFEC(true)
	_ = enc.SetPacketLossPerc(20)

	lookahead, _ := enc.Lookahead()

	fmt.Printf(" [Encoder] Sample Rate : %d Hz (%d channels)\n", sampleRate, channels)
	fmt.Printf(" [Encoder] Bitrate     : %d bps\n", targetBitrate)
	fmt.Printf(" [Encoder] VBR Enabled : true\n")
	fmt.Printf(" [Encoder] Complexity  : %d / 10\n", targetComplexity)
	fmt.Printf(" [Encoder] In-Band FEC : true (20%% expected loss)\n")
	fmt.Printf(" [Encoder] Lookahead   : %d samples\n\n", lookahead)

	// Generate and encode 5 consecutive frames (int16 PCM)
	type packetData struct {
		frameIdx int
		data     []byte
		origPCM  []int16
	}

	packets := make([]packetData, totalFrames)
	totalRawBytes := 0
	totalCompressedBytes := 0

	for f := 0; f < totalFrames; f++ {
		pcm := make([]int16, frameSize20ms*channels)
		for i := 0; i < frameSize20ms; i++ {
			t := float64(f*frameSize20ms+i) / float64(sampleRate)
			// Multi-frequency harmonic tone (300 Hz + 600 Hz)
			v := (math.Sin(2*math.Pi*300*t) + 0.5*math.Sin(2*math.Pi*600*t)) / 1.5
			val := int16(v * 20000)
			pcm[i*channels] = val
			pcm[i*channels+1] = val
		}

		pktBuf := make([]byte, 1275)
		n, err := enc.Encode(pcm, frameSize20ms, pktBuf)
		if err != nil {
			log.Fatalf("Encode frame %d failed: %v", f+1, err)
		}

		encRange, _ := enc.FinalRange()
		hasLBRR, _ := opus.PacketHasLBRR(pktBuf[:n])

		packets[f] = packetData{
			frameIdx: f + 1,
			data:     pktBuf[:n],
			origPCM:  pcm,
		}

		rawBytes := len(pcm) * 2
		totalRawBytes += rawBytes
		totalCompressedBytes += n

		fmt.Printf(" [Encoder] Frame %d: raw %d bytes -> compressed %3d bytes (FEC/LBRR: %-5v, Range: 0x%08x)\n",
			f+1, rawBytes, n, hasLBRR, encRange)
	}

	ratio := float64(totalRawBytes) / float64(totalCompressedBytes)
	fmt.Printf(" [Encoder] Total Compression Ratio: %.2f:1 (%.1f%% space saving)\n\n",
		ratio, (1.0-float64(totalCompressedBytes)/float64(totalRawBytes))*100)

	// 3. Decoding & Simulating Packet Loss Concealment (PLC)
	fmt.Println("--- Step 3: Decoding & Packet Loss Concealment (PLC) ---")
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		log.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Close()

	for _, p := range packets {
		outPCM := make([]int16, frameSize20ms*channels)

		// Intentionally drop Frame 3 to trigger PLC
		if p.frameIdx == 3 {
			// Call Decode with nil packet to conceal loss
			samples, err := dec.Decode(nil, outPCM, frameSize20ms, false)
			if err != nil {
				log.Fatalf("PLC decode failed: %v", err)
			}
			fmt.Printf(" [Decoder] Frame %d: [DROPPED / LOSS] -> PLC concealed %d samples smoothly!\n",
				p.frameIdx, samples)
			continue
		}

		samples, err := dec.Decode(p.data, outPCM, frameSize20ms, false)
		if err != nil {
			log.Fatalf("Decode frame %d failed: %v", p.frameIdx, err)
		}

		decRange, _ := dec.FinalRange()

		// Calculate SNR for received frame
		snr := computeSNR(p.origPCM, outPCM)
		fmt.Printf(" [Decoder] Frame %d: decoded %d samples, SNR: %5.1f dB (Range: 0x%08x)\n",
			p.frameIdx, samples, snr, decRange)
	}

	fmt.Println()
	fmt.Println("================================================================")
	fmt.Println(" Opus Roundtrip PCM demonstration completed successfully.")
	fmt.Println("================================================================")
}

func computeSNR(ref, dec []int16) float64 {
	n := min(len(ref), len(dec))
	var sigPower, noisePower float64
	for i := 0; i < n; i++ {
		r := float64(ref[i])
		d := float64(dec[i])
		diff := r - d
		sigPower += r * r
		noisePower += diff * diff
	}
	if noisePower == 0 {
		return 100.0
	}
	if sigPower == 0 {
		return 0.0
	}
	return 10.0 * math.Log10(sigPower/noisePower)
}
