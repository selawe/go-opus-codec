package opus

import (
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
