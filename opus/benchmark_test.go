package opus

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkDecodeInt16(b *testing.B) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		b.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	input := generateSineWave(440, 48000, 2, 960)
	packet := make([]byte, 1275)
	nBytes, err := enc.Encode(input, 960, packet)
	if err != nil {
		b.Fatalf("Encode: %v", err)
	}

	pcm := make([]int16, 960*2)
	// Warm up the decoder's ccgo/TLS pseudostack (heap+key maps, 64KB stack
	// chunk) before timing: its first Decode call is where those lazily
	// grow, and leaving it inside the loop misattributes that one-time cost
	// as steady-state per-op allocation (see BenchmarkDecode_PLC for the
	// same pattern already applied to its decoder).
	if _, err := dec.Decode(packet[:nBytes], pcm, 960, false); err != nil {
		b.Fatalf("Decode warm-up: %v", err)
	}

	b.SetBytes(int64(960 * 2 * 2)) // 960 samples * 2 channels * 2 bytes = 3840 bytes of PCM
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := dec.Decode(packet[:nBytes], pcm, 960, false)
		if err != nil {
			b.Fatalf("Decode: %v", err)
		}
	}
}

func BenchmarkDecodeFloat32(b *testing.B) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		b.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	input := generateSineWave(440, 48000, 2, 960)
	packet := make([]byte, 1275)
	nBytes, err := enc.Encode(input, 960, packet)
	if err != nil {
		b.Fatalf("Encode: %v", err)
	}

	pcm := make([]float32, 960*2)
	// Warm up the decoder's TLS pseudostack before timing (see BenchmarkDecodeInt16).
	if _, err := dec.DecodeF32(packet[:nBytes], pcm, 960, false); err != nil {
		b.Fatalf("DecodeF32 warm-up: %v", err)
	}

	b.SetBytes(int64(960 * 2 * 4)) // 960 samples * 2 channels * 4 bytes
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := dec.DecodeF32(packet[:nBytes], pcm, 960, false)
		if err != nil {
			b.Fatalf("DecodeF32: %v", err)
		}
	}
}

func BenchmarkEncodeComplexity0(b *testing.B) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()
	_ = enc.SetComplexity(0)

	input := generateSineWave(440, 48000, 2, 960)
	packet := make([]byte, 1275)

	// Warm up the encoder's TLS pseudostack before timing (see BenchmarkDecodeInt16).
	if _, err := enc.Encode(input, 960, packet); err != nil {
		b.Fatalf("Encode warm-up: %v", err)
	}

	b.SetBytes(int64(960 * 2 * 2))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := enc.Encode(input, 960, packet)
		if err != nil {
			b.Fatalf("Encode: %v", err)
		}
	}
}

func BenchmarkEncodeComplexity10(b *testing.B) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()
	_ = enc.SetComplexity(10)

	input := generateSineWave(440, 48000, 2, 960)
	packet := make([]byte, 1275)

	// Warm up the encoder's TLS pseudostack before timing (see BenchmarkDecodeInt16).
	if _, err := enc.Encode(input, 960, packet); err != nil {
		b.Fatalf("Encode warm-up: %v", err)
	}

	b.SetBytes(int64(960 * 2 * 2))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := enc.Encode(input, 960, packet)
		if err != nil {
			b.Fatalf("Encode: %v", err)
		}
	}
}

// ---- Parallel / concurrency benchmarks ----
//
// Each goroutine gets its own Encoder/Decoder instance (mirroring the
// intended usage pattern: one instance per stream/connection), so these
// benchmarks exercise the shared ccgo/libc TLS runtime under concurrent
// use rather than mutex contention on a single shared instance. This is
// the kind of workload that would surface a concurrency regression from
// changes to the TLS heap/key allocation path.
func BenchmarkEncodeParallel(b *testing.B) {
	input := generateSineWave(440, 48000, 2, 960)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		enc, err := NewEncoder(48000, 2, ApplicationAudio)
		if err != nil {
			b.Fatalf("NewEncoder: %v", err)
		}
		defer enc.Close()
		if err := enc.SetBitrate(64000); err != nil {
			b.Fatalf("SetBitrate: %v", err)
		}
		if err := enc.SetComplexity(10); err != nil {
			b.Fatalf("SetComplexity: %v", err)
		}
		packet := make([]byte, 1275)

		// Warm up this goroutine's encoder TLS pseudostack (see BenchmarkDecodeInt16).
		if _, err := enc.Encode(input, 960, packet); err != nil {
			b.Fatalf("Encode warm-up: %v", err)
		}

		for pb.Next() {
			if _, err := enc.Encode(input, 960, packet); err != nil {
				b.Fatalf("Encode: %v", err)
			}
		}
	})
}

func BenchmarkDecodeParallel(b *testing.B) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	input := generateSineWave(440, 48000, 2, 960)
	packet := make([]byte, 1275)
	nBytes, err := enc.Encode(input, 960, packet)
	if err != nil {
		b.Fatalf("Encode: %v", err)
	}
	packet = packet[:nBytes]

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		dec, err := NewDecoder(48000, 2)
		if err != nil {
			b.Fatalf("NewDecoder: %v", err)
		}
		defer dec.Close()
		pcm := make([]int16, 960*2)

		// Warm up this goroutine's decoder TLS pseudostack (see BenchmarkDecodeInt16).
		if _, err := dec.Decode(packet, pcm, 960, false); err != nil {
			b.Fatalf("Decode warm-up: %v", err)
		}

		for pb.Next() {
			if _, err := dec.Decode(packet, pcm, 960, false); err != nil {
				b.Fatalf("Decode: %v", err)
			}
		}
	})
}

func BenchmarkPacketParse(b *testing.B) {
	pkt := []byte{0xFC, 0x01, 0x02, 0x03}
	b.ResetTimer()
	b.ReportAllocs()

	var d time.Duration
	for i := 0; i < b.N; i++ {
		d, _ = PacketDuration(pkt)
	}
	_ = d
}

// ---- Phase 1: Real-World Configuration Variations Matrix ----

type benchConfig struct {
	name        string
	sampleRate  int
	channels    int
	frameSize   int
	bitrate     int
	application int
}

func BenchmarkDecode_Matrix(b *testing.B) {
	configs := []benchConfig{
		{"VoIP_16k_Mono_10ms", 16000, 1, 160, 24000, ApplicationVoIP},
		{"VoIP_16k_Mono_20ms", 16000, 1, 320, 24000, ApplicationVoIP},
		{"VoIP_16k_Mono_40ms", 16000, 1, 640, 24000, ApplicationVoIP},
		{"VoIP_16k_Mono_60ms", 16000, 1, 960, 24000, ApplicationVoIP},
		{"Audio_48k_Stereo_2.5ms", 48000, 2, 120, 64000, ApplicationAudio},
		{"Audio_48k_Stereo_5ms", 48000, 2, 240, 64000, ApplicationAudio},
		{"Audio_48k_Stereo_10ms", 48000, 2, 480, 64000, ApplicationAudio},
		{"Audio_48k_Stereo_20ms", 48000, 2, 960, 64000, ApplicationAudio},
		{"Audio_48k_Stereo_40ms", 48000, 2, 1920, 64000, ApplicationAudio},
		{"Audio_48k_Stereo_60ms", 48000, 2, 2880, 64000, ApplicationAudio},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			enc, err := NewEncoder(cfg.sampleRate, cfg.channels, cfg.application)
			if err != nil {
				b.Fatalf("NewEncoder: %v", err)
			}
			defer enc.Close()
			_ = enc.SetBitrate(cfg.bitrate)

			dec, err := NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				b.Fatalf("NewDecoder: %v", err)
			}
			defer dec.Close()

			input := generateSineWave(440, cfg.sampleRate, cfg.channels, cfg.frameSize)
			packet := make([]byte, 1275)
			nBytes, err := enc.Encode(input, cfg.frameSize, packet)
			if err != nil {
				b.Fatalf("Encode: %v", err)
			}

			pcm := make([]int16, 5760*cfg.channels)
			// Warm up the decoder's TLS pseudostack before timing (see BenchmarkDecodeInt16).
			if _, err := dec.Decode(packet[:nBytes], pcm, 5760, false); err != nil {
				b.Fatalf("Decode warm-up: %v", err)
			}

			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := dec.Decode(packet[:nBytes], pcm, 5760, false)
				if err != nil {
					b.Fatalf("Decode: %v", err)
				}
			}
		})
	}
}

func BenchmarkEncode_Matrix(b *testing.B) {
	configs := []struct {
		benchConfig
		complexities []int
	}{
		{
			benchConfig:  benchConfig{"VoIP_16k_Mono_20ms", 16000, 1, 320, 24000, ApplicationVoIP},
			complexities: []int{1, 5, 10},
		},
		{
			benchConfig:  benchConfig{"VoIP_16k_Mono_60ms", 16000, 1, 960, 24000, ApplicationVoIP},
			complexities: []int{10},
		},
		{
			benchConfig:  benchConfig{"Audio_48k_Stereo_2.5ms", 48000, 2, 120, 64000, ApplicationAudio},
			complexities: []int{10},
		},
		{
			benchConfig:  benchConfig{"Audio_48k_Stereo_5ms", 48000, 2, 240, 64000, ApplicationAudio},
			complexities: []int{10},
		},
		{
			benchConfig:  benchConfig{"Audio_48k_Stereo_10ms", 48000, 2, 480, 64000, ApplicationAudio},
			complexities: []int{10},
		},
		{
			benchConfig:  benchConfig{"Audio_48k_Stereo_20ms", 48000, 2, 960, 64000, ApplicationAudio},
			complexities: []int{1, 5, 10},
		},
		{
			benchConfig:  benchConfig{"Audio_48k_Stereo_40ms", 48000, 2, 1920, 64000, ApplicationAudio},
			complexities: []int{10},
		},
		{
			benchConfig:  benchConfig{"Audio_48k_Stereo_60ms", 48000, 2, 2880, 64000, ApplicationAudio},
			complexities: []int{10},
		},
	}

	for _, tc := range configs {
		cfg := tc.benchConfig
		for _, comp := range tc.complexities {
			name := fmt.Sprintf("%s_c%d", cfg.name, comp)
			b.Run(name, func(b *testing.B) {
				enc, err := NewEncoder(cfg.sampleRate, cfg.channels, cfg.application)
				if err != nil {
					b.Fatalf("NewEncoder: %v", err)
				}
				defer enc.Close()
				_ = enc.SetBitrate(cfg.bitrate)
				_ = enc.SetComplexity(comp)

				input := generateSineWave(440, cfg.sampleRate, cfg.channels, cfg.frameSize)
				packet := make([]byte, 1275)

				// Warm up the encoder's TLS pseudostack before timing (see BenchmarkDecodeInt16).
				if _, err := enc.Encode(input, cfg.frameSize, packet); err != nil {
					b.Fatalf("Encode warm-up: %v", err)
				}

				b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					_, err := enc.Encode(input, cfg.frameSize, packet)
					if err != nil {
						b.Fatalf("Encode: %v", err)
					}
				}
			})
		}
	}
}

// ---- Phase 2: Realtime Network Features (PLC, Repacketizer, Padding) ----

func BenchmarkDecode_PLC(b *testing.B) {
	configs := []struct {
		name       string
		sampleRate int
		channels   int
		frameSize  int
		app        int
	}{
		{"VoIP_16k_Mono_20ms", 16000, 1, 320, ApplicationVoIP},
		{"Audio_48k_Stereo_20ms", 48000, 2, 960, ApplicationAudio},
	}

	for _, cfg := range configs {
		b.Run(cfg.name+"_Normal", func(b *testing.B) {
			enc, err := NewEncoder(cfg.sampleRate, cfg.channels, cfg.app)
			if err != nil {
				b.Fatalf("NewEncoder: %v", err)
			}
			defer enc.Close()

			dec, err := NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				b.Fatalf("NewDecoder: %v", err)
			}
			defer dec.Close()

			input := generateSineWave(440, cfg.sampleRate, cfg.channels, cfg.frameSize)
			pkt := make([]byte, 1275)
			nBytes, err := enc.Encode(input, cfg.frameSize, pkt)
			if err != nil {
				b.Fatalf("Encode: %v", err)
			}

			pcm := make([]int16, 5760*cfg.channels)
			// Warm up the decoder's TLS pseudostack before timing, matching the
			// _PLC variant below so the two are an apples-to-apples comparison
			// (see BenchmarkDecodeInt16 for why this matters).
			if _, err := dec.Decode(pkt[:nBytes], pcm, 5760, false); err != nil {
				b.Fatalf("Decode warm-up: %v", err)
			}

			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := dec.Decode(pkt[:nBytes], pcm, 5760, false)
				if err != nil {
					b.Fatalf("Decode: %v", err)
				}
			}
		})

		b.Run(cfg.name+"_PLC", func(b *testing.B) {
			enc, err := NewEncoder(cfg.sampleRate, cfg.channels, cfg.app)
			if err != nil {
				b.Fatalf("NewEncoder: %v", err)
			}
			defer enc.Close()

			dec, err := NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				b.Fatalf("NewDecoder: %v", err)
			}
			defer dec.Close()

			// Prime the decoder with 1 normal frame so state contains speech/audio history
			input := generateSineWave(440, cfg.sampleRate, cfg.channels, cfg.frameSize)
			pkt := make([]byte, 1275)
			nBytes, err := enc.Encode(input, cfg.frameSize, pkt)
			if err != nil {
				b.Fatalf("Encode: %v", err)
			}

			pcm := make([]int16, 5760*cfg.channels)
			if _, err := dec.Decode(pkt[:nBytes], pcm, 5760, false); err != nil {
				b.Fatalf("Decode prime: %v", err)
			}

			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := dec.Decode(nil, pcm, cfg.frameSize, false)
				if err != nil {
					b.Fatalf("PLC Decode: %v", err)
				}
			}
		})
	}
}

// BenchmarkDecode_FEC exercises in-band FEC recovery: the encoder is
// configured with SetInbandFEC + SetPacketLossPerc, and per RFC 6716's
// LBRR mechanism (opus_decode's decode_fec argument), the *next* packet
// after a loss carries redundant data for the lost frame. Decode is
// called with decodeFEC=true on that next packet to recover the "lost"
// frame's audio, simulating packet-loss recovery rather than the silent
// concealment covered by BenchmarkDecode_PLC.
func BenchmarkDecode_FEC(b *testing.B) {
	configs := []struct {
		name       string
		sampleRate int
		channels   int
		frameSize  int
		app        int
	}{
		{"VoIP_16k_Mono_20ms", 16000, 1, 320, ApplicationVoIP},
		{"Audio_48k_Stereo_20ms", 48000, 2, 960, ApplicationAudio},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			enc, err := NewEncoder(cfg.sampleRate, cfg.channels, cfg.app)
			if err != nil {
				b.Fatalf("NewEncoder: %v", err)
			}
			defer enc.Close()
			if err := enc.SetInbandFEC(true); err != nil {
				b.Fatalf("SetInbandFEC: %v", err)
			}
			if err := enc.SetPacketLossPerc(10); err != nil {
				b.Fatalf("SetPacketLossPerc: %v", err)
			}

			dec, err := NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				b.Fatalf("NewDecoder: %v", err)
			}
			defer dec.Close()

			// frame1 is the "lost" frame; frame2 is the next packet, which
			// carries LBRR redundancy for frame1 when decoded with fec=true.
			input1 := generateSineWave(440, cfg.sampleRate, cfg.channels, cfg.frameSize)
			input2 := generateSineWave(460, cfg.sampleRate, cfg.channels, cfg.frameSize)
			pkt1 := make([]byte, 1275)
			pkt2 := make([]byte, 1275)
			if _, err := enc.Encode(input1, cfg.frameSize, pkt1); err != nil {
				b.Fatalf("Encode frame1: %v", err)
			}
			n2, err := enc.Encode(input2, cfg.frameSize, pkt2)
			if err != nil {
				b.Fatalf("Encode frame2: %v", err)
			}
			pkt2 = pkt2[:n2]

			pcm := make([]int16, 5760*cfg.channels)
			// Warm up the decoder's TLS pseudostack before timing (see BenchmarkDecodeInt16).
			if _, err := dec.Decode(pkt2, pcm, cfg.frameSize, true); err != nil {
				b.Fatalf("FEC Decode warm-up: %v", err)
			}

			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := dec.Decode(pkt2, pcm, cfg.frameSize, true)
				if err != nil {
					b.Fatalf("FEC Decode: %v", err)
				}
			}
		})
	}
}

func BenchmarkRepacketizer_Merge(b *testing.B) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	var packets [][]byte
	for f := 0; f < 3; f++ {
		pcm := generateSineWave(440+float64(f*50), 48000, 2, 960)
		pkt := make([]byte, 1000)
		n, err := enc.Encode(pcm, 960, pkt)
		if err != nil {
			b.Fatalf("Encode: %v", err)
		}
		packets = append(packets, pkt[:n])
	}

	rp, err := NewRepacketizer()
	if err != nil {
		b.Fatalf("NewRepacketizer: %v", err)
	}
	defer rp.Close()

	dst := make([]byte, 4000)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = rp.Reset()
		for _, pkt := range packets {
			if err := rp.Cat(pkt); err != nil {
				b.Fatalf("Cat: %v", err)
			}
		}
		if _, err := rp.Out(dst); err != nil {
			b.Fatalf("Out: %v", err)
		}
	}
}

func BenchmarkRepacketizer_Split(b *testing.B) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	rp, err := NewRepacketizer()
	if err != nil {
		b.Fatalf("NewRepacketizer: %v", err)
	}
	defer rp.Close()

	for f := 0; f < 3; f++ {
		pcm := generateSineWave(440+float64(f*50), 48000, 2, 960)
		pkt := make([]byte, 1000)
		n, err := enc.Encode(pcm, 960, pkt)
		if err != nil {
			b.Fatalf("Encode: %v", err)
		}
		_ = rp.Cat(pkt[:n])
	}

	merged := make([]byte, 4000)
	nMerged, err := rp.Out(merged)
	if err != nil {
		b.Fatalf("Out: %v", err)
	}
	mergedPacket := merged[:nMerged]

	dst := make([]byte, 1500)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = rp.Reset()
		if err := rp.Cat(mergedPacket); err != nil {
			b.Fatalf("Cat: %v", err)
		}
		if _, err := rp.OutRange(0, 1, dst); err != nil {
			b.Fatalf("OutRange: %v", err)
		}
	}
}

func BenchmarkSoftClip(b *testing.B) {
	pcm := make([]float32, 960*2)
	for i := range pcm {
		pcm[i] = 0.9
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := SoftClip(pcm, 2); err != nil {
			b.Fatalf("SoftClip: %v", err)
		}
	}
}

func BenchmarkPacketPad(b *testing.B) {
	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	pcm := generateSineWave(440, 48000, 2, 960)
	raw := make([]byte, 1000)
	n, err := enc.Encode(pcm, 960, raw)
	if err != nil {
		b.Fatalf("Encode: %v", err)
	}
	packet := raw[:n]

	b.Run("Pad_500B", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := PacketPad(packet, 500); err != nil {
				b.Fatalf("PacketPad: %v", err)
			}
		}
	})

	padded, err := PacketPad(packet, 500)
	if err != nil {
		b.Fatalf("PacketPad setup: %v", err)
	}

	b.Run("Unpad_500B", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := PacketUnpad(padded); err != nil {
				b.Fatalf("PacketUnpad: %v", err)
			}
		}
	})
}
