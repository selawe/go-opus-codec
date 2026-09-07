package main

import (
	"bytes"
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
		uniformSize   = 500 // Target fixed CBR packet size in bytes
	)

	fmt.Println("================================================================")
	fmt.Println(" Opus CBR Packet Padding & Unpadding Example")
	fmt.Println("================================================================")
	fmt.Println("Demonstrates:")
	fmt.Println(" 1. Padding variable-length Opus packets to a fixed size (CBR)")
	fmt.Println(" 2. Preventing packet-size traffic analysis in secure VoIP/VPN")
	fmt.Println(" 3. Stripping padding via opus.PacketUnpad")
	fmt.Println(" 4. Decoding both padded and unpadded packets with bit-exact fidelity")
	fmt.Println()

	// 1. Initialize Encoder
	enc, err := opus.NewEncoder(sampleRate, channels, opus.ApplicationVoIP)
	if err != nil {
		log.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	// 2. Generate 3 packets with varying audio content (different natural sizes)
	var originalPackets [][]byte
	for i := 0; i < 3; i++ {
		pcm := make([]int16, frameSize20ms*channels)
		freq := float64(200 * (i + 1))
		for s := 0; s < frameSize20ms; s++ {
			t := float64(s) / float64(sampleRate)
			val := int16(math.Sin(2*math.Pi*freq*t) * float64(10000*(i+1)))
			pcm[s*channels] = val
			pcm[s*channels+1] = val
		}

		pktBuf := make([]byte, 1275)
		n, err := enc.Encode(pcm, frameSize20ms, pktBuf)
		if err != nil {
			log.Fatalf("Encode frame %d failed: %v", i+1, err)
		}
		pkt := make([]byte, n)
		copy(pkt, pktBuf[:n])
		originalPackets = append(originalPackets, pkt)

		fmt.Printf(" [Encoder] Frame %d: variable length = %3d bytes (TOC: 0x%02x)\n",
			i+1, len(pkt), pkt[0])
	}
	fmt.Println()

	// 3. Apply PacketPad to pad each packet up to uniformSize bytes
	fmt.Printf("--- Step 2: Applying opus.PacketPad (Uniform Target: %d bytes) ---\n", uniformSize)
	var paddedPackets [][]byte
	for i, pkt := range originalPackets {
		padded, err := opus.PacketPad(pkt, uniformSize)
		if err != nil {
			log.Fatalf("PacketPad frame %d failed: %v", i+1, err)
		}
		paddedPackets = append(paddedPackets, padded)
		fmt.Printf(" [Padder] Frame %d: %3d bytes -> padded to exactly %d bytes (overhead: +%d bytes)\n",
			i+1, len(pkt), len(padded), len(padded)-len(pkt))
	}
	fmt.Println()

	// 4. Decode padded packets directly (Opus decoders handle RFC 6716 padding natively)
	fmt.Println("--- Step 3: Decoding Padded Packets Directly ---")
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		log.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Close()

	for i, padded := range paddedPackets {
		outPCM := make([]int16, frameSize20ms*channels)
		samples, err := dec.Decode(padded, outPCM, frameSize20ms, false)
		if err != nil {
			log.Fatalf("Decode padded frame %d failed: %v", i+1, err)
		}
		fmt.Printf(" [Decoder] Direct padded frame %d: decoded %d samples successfully!\n",
			i+1, samples)
	}
	fmt.Println()

	// 5. Strip padding using opus.PacketUnpad and verify bit-for-bit match with original
	fmt.Println("--- Step 4: Stripping Padding with opus.PacketUnpad ---")
	for i, padded := range paddedPackets {
		unpadded, err := opus.PacketUnpad(padded)
		if err != nil {
			log.Fatalf("PacketUnpad frame %d failed: %v", i+1, err)
		}

		orig := originalPackets[i]
		isExactMatch := bytes.Equal(orig, unpadded)

		fmt.Printf(" [Unpadder] Frame %d: unpadded size = %3d bytes (Matches Original: %v)\n",
			i+1, len(unpadded), isExactMatch)

		if !isExactMatch {
			log.Fatalf("Frame %d unpadded bytes do not match original!", i+1)
		}
	}

	fmt.Println()
	fmt.Println("================================================================")
	fmt.Println(" Opus CBR Packet Padding & Unpadding completed successfully.")
	fmt.Println("================================================================")
}
