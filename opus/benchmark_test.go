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
		{"Audio_48k_Stereo_10ms", 48000, 2, 480, 64000, ApplicationAudio},
		{"Audio_48k_Stereo_20ms", 48000, 2, 960, 64000, ApplicationAudio},
		{"Audio_48k_Stereo_40ms", 48000, 2, 1920, 64000, ApplicationAudio},
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
			benchConfig:  benchConfig{"Audio_48k_Stereo_20ms", 48000, 2, 960, 64000, ApplicationAudio},
			complexities: []int{1, 5, 10},
		},
		{
			benchConfig:  benchConfig{"Audio_48k_Stereo_10ms", 48000, 2, 480, 64000, ApplicationAudio},
			complexities: []int{10},
		},
		{
			benchConfig:  benchConfig{"Audio_48k_Stereo_40ms", 48000, 2, 1920, 64000, ApplicationAudio},
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
