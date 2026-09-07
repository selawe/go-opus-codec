package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
)

func main() {
	const (
		sampleRate    = 48000
		channels      = 2
		frameSize20ms = 960 // 20ms @ 48kHz
		totalFrames   = 10  // 200ms of audio
		serial        = uint32(0x12345678)
	)

	fmt.Println("================================================================")
	fmt.Println(" Ogg Opus Stream Mux & Demux Example (Pure Go Container)")
	fmt.Println("================================================================")
	fmt.Println("Demonstrates:")
	fmt.Println(" 1. Muxing an RFC 7845 / RFC 3533 Ogg Opus stream in pure Go")
	fmt.Println(" 2. Generating OpusHead, PreSkip lookahead, and OpusTags metadata")
	fmt.Println(" 3. Demuxing and inspecting container headers with ogg.OpusReader")
	fmt.Println(" 4. Decoding stream packets back to PCM samples")
	fmt.Println()

	// --- STEP 1: MUXING (ENCODE & WRITE OGG OPUS) ---
	fmt.Println("--- Step 1: Muxing Ogg Opus Stream ---")
	enc, err := opus.NewEncoder(sampleRate, channels, opus.ApplicationAudio)
	if err != nil {
		log.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()
	_ = enc.SetBitrate(64000)

	lookahead, err := enc.Lookahead()
	if err != nil {
		log.Fatalf("Lookahead error: %v", err)
	}

	var oggBuffer bytes.Buffer
	pw := ogg.NewPacketWriter(&oggBuffer, serial)

	// 1.1 Write OpusHead packet (mandatory first packet in BOS page)
	head := ogg.OpusHead{
		Version:              1,
		Channels:             uint8(channels),
		PreSkip:              uint16(lookahead),
		InputSampleRate:      sampleRate,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	headPacket, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		log.Fatalf("BuildOpusHeadPacket failed: %v", err)
	}
	if err := pw.WritePacket(headPacket, 0, true, false); err != nil {
		log.Fatalf("Write OpusHead failed: %v", err)
	}
	fmt.Printf(" [Muxer] Wrote OpusHead: channels=%d, preSkip=%d, inputRate=%d\n",
		head.Channels, head.PreSkip, head.InputSampleRate)

	// 1.2 Write OpusTags packet (mandatory second packet)
	tags := ogg.OpusTags{
		Vendor: "go-opus-codec",
		Comments: []string{
			"TITLE=Synthetic Tone Stream",
			"ARTIST=go-opus-codec Example",
			"GENRE=Audio Test",
			"ENCODER=github.com/selawe/go-opus-codec",
		},
	}
	tagsPacket, err := ogg.BuildOpusTagsPacket(tags)
	if err != nil {
		log.Fatalf("BuildOpusTagsPacket failed: %v", err)
	}
	if err := pw.WritePacket(tagsPacket, 0, false, false); err != nil {
		log.Fatalf("Write OpusTags failed: %v", err)
	}
	fmt.Printf(" [Muxer] Wrote OpusTags: %d comments, vendor=%q\n",
		len(tags.Comments), tags.Vendor)

	// 1.3 Encode and write audio packets
	var totalSamples uint64
	for f := 0; f < totalFrames; f++ {
		pcm := make([]int16, frameSize20ms*channels)
		for i := 0; i < frameSize20ms; i++ {
			t := float64(f*frameSize20ms+i) / float64(sampleRate)
			val := int16(math.Sin(2*math.Pi*440.0*t) * 16000)
			pcm[i*channels] = val
			pcm[i*channels+1] = val
		}

		pktBuf := make([]byte, 1275)
		n, err := enc.Encode(pcm, frameSize20ms, pktBuf)
		if err != nil {
			log.Fatalf("Encode frame %d failed: %v", f+1, err)
		}

		totalSamples += uint64(frameSize20ms)
		granulePos := uint64(head.PreSkip) + totalSamples
		isLast := (f == totalFrames-1)

		if err := pw.WritePacket(pktBuf[:n], granulePos, false, isLast); err != nil {
			log.Fatalf("WritePacket frame %d failed: %v", f+1, err)
		}
	}

	if err := pw.Flush(); err != nil {
		log.Fatalf("Flush failed: %v", err)
	}

	fmt.Printf(" [Muxer] Successfully wrote %d audio frames into Ogg stream (%d bytes total)\n\n",
		totalFrames, oggBuffer.Len())

	// --- STEP 2: DEMUXING & READING OGG OPUS ---
	fmt.Println("--- Step 2: Demuxing & Reading Stream ---")
	reader, err := ogg.NewOpusReader(bytes.NewReader(oggBuffer.Bytes()))
	if err != nil {
		log.Fatalf("NewOpusReader failed: %v", err)
	}

	// Inspect parsed metadata headers
	fmt.Printf(" [Reader] Parsed OpusHead:\n")
	fmt.Printf("          Version        : %d\n", reader.Head.Version)
	fmt.Printf("          Channels       : %d\n", reader.Head.Channels)
	fmt.Printf("          PreSkip        : %d samples\n", reader.Head.PreSkip)
	fmt.Printf("          Input Rate     : %d Hz\n", reader.Head.InputSampleRate)
	fmt.Printf("          Mapping Family : %d\n", reader.Head.ChannelMappingFamily)
	fmt.Printf(" [Reader] Parsed OpusTags:\n")
	fmt.Printf("          Vendor         : %s\n", reader.Tags.Vendor)
	fmt.Printf("          Title          : %s\n", reader.Tags.Get("TITLE"))
	fmt.Printf("          Artist         : %s\n", reader.Tags.Get("ARTIST"))
	fmt.Printf("          Genre          : %s\n", reader.Tags.Get("GENRE"))
	fmt.Printf("          Encoder        : %s\n\n", reader.Tags.Get("ENCODER"))

	// --- STEP 3: DECODING PACKETS ---
	fmt.Println("--- Step 3: Decoding Demuxed Audio Packets ---")
	dec, err := opus.NewDecoder(int(reader.Head.InputSampleRate), int(reader.Head.Channels))
	if err != nil {
		log.Fatalf("NewDecoder failed: %v", err)
	}
	defer dec.Close()

	packetCount := 0
	totalDecodedSamples := 0

	for {
		audioPkt, err := reader.ReadAudioPacket()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Fatalf("ReadAudioPacket failed: %v", err)
		}

		packetCount++
		outPCM := make([]int16, frameSize20ms*channels)
		samples, err := dec.Decode(audioPkt.Data, outPCM, frameSize20ms, false)
		if err != nil {
			log.Fatalf("Decode packet %d failed: %v", packetCount, err)
		}
		totalDecodedSamples += samples

		fmt.Printf(" [Reader] Packet %2d: %3d bytes, Granule: %5d, Decoded: %d samples (EOS: %v)\n",
			packetCount, len(audioPkt.Data), audioPkt.GranulePos, samples, audioPkt.EOS)
	}

	fmt.Println()
	fmt.Printf(" [Summary] Total Packets Read  : %d\n", packetCount)
	fmt.Printf("           Total Samples Decoded: %d per channel (%.2f ms @ %d Hz)\n",
		totalDecodedSamples, float64(totalDecodedSamples)*1000/sampleRate, sampleRate)
	fmt.Println("================================================================")
	fmt.Println(" Ogg Opus Stream Mux & Demux completed successfully.")
	fmt.Println("================================================================")
}
