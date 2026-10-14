package opus_test

import (
	"fmt"
	"log"

	"github.com/selawe/go-opus-codec/opus"
)

// Encode one 20 ms frame and decode it again. Encoders and decoders hold native-style state, so
// Close them when done.
func ExampleEncoder() {
	enc, err := opus.NewEncoder(48000, 1, opus.ApplicationVoIP)
	if err != nil {
		log.Fatal(err)
	}
	defer enc.Close()
	if err := enc.SetBitrate(24000); err != nil {
		log.Fatal(err)
	}
	if err := enc.SetSignal(opus.SignalVoice); err != nil {
		log.Fatal(err)
	}

	dec, err := opus.NewDecoder(48000, 1)
	if err != nil {
		log.Fatal(err)
	}
	defer dec.Close()

	pcm := make([]int16, 960) // 20 ms at 48 kHz, mono
	packet := make([]byte, 1275)
	n, err := enc.Encode(pcm, 960, packet)
	if err != nil {
		log.Fatal(err)
	}

	out := make([]int16, 960)
	frames, err := dec.Decode(packet[:n], out, 960, false)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("decoded frames:", frames)
	// Output:
	// decoded frames: 960
}

// A nil packet asks the decoder to conceal a lost frame (packet loss concealment).
func ExampleDecoder_Decode_packetLoss() {
	dec, err := opus.NewDecoder(48000, 2)
	if err != nil {
		log.Fatal(err)
	}
	defer dec.Close()

	out := make([]int16, 960*2)
	frames, err := dec.Decode(nil, out, 960, false)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("concealed frames:", frames)
	// Output:
	// concealed frames: 960
}

// Inspect a packet without decoding it.
func ExamplePacketTotalSamples() {
	// TOC byte 0xFC: CELT fullband, 20 ms, mono, one frame (code 0), followed by one payload byte.
	packet := []byte{0xFC, 0x00}

	frames, err := opus.PacketFrames(packet)
	if err != nil {
		log.Fatal(err)
	}
	samples, err := opus.PacketTotalSamples(packet, 48000)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("frames:", len(frames), "samples per channel:", samples)
	// Output:
	// frames: 1 samples per channel: 960
}
