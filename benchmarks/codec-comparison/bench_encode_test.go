package benchcompare

import (
	"math"
	"math/rand"
	"testing"

	hraban "github.com/hraban/opus"
	goopus "github.com/selawe/go-opus-codec/opus"
)

// Encode benchmark settings shared by both encoders under comparison.
//
// Note: pion/opus (github.com/pion/opus) has no public Encoder at all as of
// v0.1.0 - only internal/celt/encoder.go, used solely by its own entropy
// coder tests and not exported. So this file only compares go-opus-codec
// against libopus (via cgo); see bench_decode_test.go for the 3-way decode
// comparison, where pion/opus does participate.
const (
	encSampleRate  = 48000
	encChannels    = 2
	encFrameSize   = 960 // 20ms @ 48kHz
	encBitrate     = 64000
	encComplexity  = 10
	encApplication = goopus.ApplicationAudio // == 2049, matches libopus's OPUS_APPLICATION_AUDIO
)

// encodePCMCorpus holds synthetic interleaved stereo 20ms frames: a sweep of
// sine waves at varying frequency plus a bit of seeded noise, so the encoder
// sees some varied signal content instead of pure silence.
var encodePCMCorpus [][]int16

func init() {
	rng := rand.New(rand.NewSource(1))
	const numFrames = 50
	encodePCMCorpus = make([][]int16, numFrames)
	for f := 0; f < numFrames; f++ {
		freq := 220.0 + float64(f)*30.0 // sweep 220Hz..1690Hz across frames
		frame := make([]int16, encFrameSize*encChannels)
		for i := 0; i < encFrameSize; i++ {
			t := float64(i) / float64(encSampleRate)
			sample := 0.6 * math.Sin(2*math.Pi*freq*t)
			sample += 0.02 * (rng.Float64()*2 - 1) // light dither/noise
			v := int16(sample * 32767)
			frame[i*2] = v   // left
			frame[i*2+1] = v // right
		}
		encodePCMCorpus[f] = frame
	}
}

// TestEncodeRoundTrips is a correctness sanity check that must pass before
// the benchmark numbers below are trusted: each encoder must produce a
// non-empty packet, and this repo's own decoder must be able to decode it
// back without error (proving the encoder settings actually work end to end,
// not just "didn't return an error").
func TestEncodeRoundTrips(t *testing.T) {
	frame := encodePCMCorpus[0]

	dec, err := goopus.NewDecoder(encSampleRate, encChannels)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()
	pcmOut := make([]int16, 5760*encChannels)

	t.Run("GoOpusCodec", func(t *testing.T) {
		enc, err := goopus.NewEncoder(encSampleRate, encChannels, encApplication)
		if err != nil {
			t.Fatalf("NewEncoder: %v", err)
		}
		defer enc.Close()
		if err := enc.SetBitrate(encBitrate); err != nil {
			t.Fatalf("SetBitrate: %v", err)
		}
		if err := enc.SetComplexity(encComplexity); err != nil {
			t.Fatalf("SetComplexity: %v", err)
		}
		packet := make([]byte, 4000)
		n, err := enc.Encode(frame, encFrameSize, packet)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if n <= 0 {
			t.Fatalf("Encode produced empty packet")
		}
		if _, err := dec.Decode(packet[:n], pcmOut, 5760, false); err != nil {
			t.Fatalf("round-trip Decode: %v", err)
		}
	})

	t.Run("Libopus", func(t *testing.T) {
		enc, err := hraban.NewEncoder(encSampleRate, encChannels, hraban.AppAudio)
		if err != nil {
			t.Fatalf("NewEncoder: %v", err)
		}
		if err := enc.SetBitrate(encBitrate); err != nil {
			t.Fatalf("SetBitrate: %v", err)
		}
		if err := enc.SetComplexity(encComplexity); err != nil {
			t.Fatalf("SetComplexity: %v", err)
		}
		packet := make([]byte, 4000)
		n, err := enc.Encode(frame, packet)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if n <= 0 {
			t.Fatalf("Encode produced empty packet")
		}
		if _, err := dec.Decode(packet[:n], pcmOut, 5760, false); err != nil {
			t.Fatalf("round-trip Decode: %v", err)
		}
	})
}

func BenchmarkEncode_GoOpusCodec(b *testing.B) {
	enc, err := goopus.NewEncoder(encSampleRate, encChannels, encApplication)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()
	if err := enc.SetBitrate(encBitrate); err != nil {
		b.Fatalf("SetBitrate: %v", err)
	}
	if err := enc.SetComplexity(encComplexity); err != nil {
		b.Fatalf("SetComplexity: %v", err)
	}
	packet := make([]byte, 4000)
	// Warm up encoder's ccgo/TLS pseudostack and staging buffer before timing
	if len(encodePCMCorpus) > 0 {
		if _, err := enc.Encode(encodePCMCorpus[0], encFrameSize, packet); err != nil {
			b.Fatalf("Encode warm-up: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame := encodePCMCorpus[i%len(encodePCMCorpus)]
		if _, err := enc.Encode(frame, encFrameSize, packet); err != nil {
			b.Fatalf("Encode: %v", err)
		}
	}
}

func BenchmarkEncode_Libopus(b *testing.B) {
	enc, err := hraban.NewEncoder(encSampleRate, encChannels, hraban.AppAudio)
	if err != nil {
		b.Fatalf("NewEncoder: %v", err)
	}
	if err := enc.SetBitrate(encBitrate); err != nil {
		b.Fatalf("SetBitrate: %v", err)
	}
	if err := enc.SetComplexity(encComplexity); err != nil {
		b.Fatalf("SetComplexity: %v", err)
	}
	packet := make([]byte, 4000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame := encodePCMCorpus[i%len(encodePCMCorpus)]
		if _, err := enc.Encode(frame, packet); err != nil {
			b.Fatalf("Encode: %v", err)
		}
	}
}

// ---- Phase 1: Real-World Matrix Encode Benchmarks ----

type matrixEncodeConfig struct {
	name       string
	sampleRate int
	channels   int
	frameSize  int
	bitrate    int
	complexity int
	goApp      int
	libopusApp hraban.Application
}

var matrixConfigs = []matrixEncodeConfig{
	{"VoIP_16k_Mono_20ms_c1", 16000, 1, 320, 24000, 1, goopus.ApplicationVoIP, hraban.AppVoIP},
	{"VoIP_16k_Mono_20ms_c5", 16000, 1, 320, 24000, 5, goopus.ApplicationVoIP, hraban.AppVoIP},
	{"VoIP_16k_Mono_20ms_c10", 16000, 1, 320, 24000, 10, goopus.ApplicationVoIP, hraban.AppVoIP},
	{"Audio_48k_Stereo_20ms_c1", 48000, 2, 960, 64000, 1, goopus.ApplicationAudio, hraban.AppAudio},
	{"Audio_48k_Stereo_20ms_c5", 48000, 2, 960, 64000, 5, goopus.ApplicationAudio, hraban.AppAudio},
	{"Audio_48k_Stereo_20ms_c10", 48000, 2, 960, 64000, 10, goopus.ApplicationAudio, hraban.AppAudio},
	{"Audio_48k_Stereo_10ms_c10", 48000, 2, 480, 64000, 10, goopus.ApplicationAudio, hraban.AppAudio},
	{"Audio_48k_Stereo_40ms_c10", 48000, 2, 1920, 64000, 10, goopus.ApplicationAudio, hraban.AppAudio},
}

func makePCMCorpus(sampleRate, channels, frameSize, numFrames int) [][]int16 {
	rng := rand.New(rand.NewSource(1))
	corpus := make([][]int16, numFrames)
	for f := 0; f < numFrames; f++ {
		freq := 220.0 + float64(f)*30.0
		frame := make([]int16, frameSize*channels)
		for i := 0; i < frameSize; i++ {
			t := float64(i) / float64(sampleRate)
			sample := 0.6 * math.Sin(2*math.Pi*freq*t)
			sample += 0.02 * (rng.Float64()*2 - 1)
			v := int16(sample * 32767)
			for c := 0; c < channels; c++ {
				frame[i*channels+c] = v
			}
		}
		corpus[f] = frame
	}
	return corpus
}

func TestMatrixEncodeRoundTrips(t *testing.T) {
	for _, cfg := range matrixConfigs {
		t.Run(cfg.name, func(t *testing.T) {
			corpus := makePCMCorpus(cfg.sampleRate, cfg.channels, cfg.frameSize, 2)
			frame := corpus[0]

			dec, err := goopus.NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				t.Fatalf("goopus NewDecoder: %v", err)
			}
			defer dec.Close()

			// Test goopus encode
			goEnc, err := goopus.NewEncoder(cfg.sampleRate, cfg.channels, cfg.goApp)
			if err != nil {
				t.Fatalf("goopus NewEncoder: %v", err)
			}
			defer goEnc.Close()
			_ = goEnc.SetBitrate(cfg.bitrate)
			_ = goEnc.SetComplexity(cfg.complexity)

			pktGo := make([]byte, 4000)
			nGo, err := goEnc.Encode(frame, cfg.frameSize, pktGo)
			if err != nil || nGo <= 0 {
				t.Fatalf("goopus Encode error: %v, n=%d", err, nGo)
			}

			pcmOut := make([]int16, 5760*cfg.channels)
			if _, err := dec.Decode(pktGo[:nGo], pcmOut, 5760, false); err != nil {
				t.Fatalf("goopus roundtrip decode error: %v", err)
			}

			// Test libopus encode
			libEnc, err := hraban.NewEncoder(cfg.sampleRate, cfg.channels, cfg.libopusApp)
			if err != nil {
				t.Fatalf("libopus NewEncoder: %v", err)
			}
			_ = libEnc.SetBitrate(cfg.bitrate)
			_ = libEnc.SetComplexity(cfg.complexity)

			pktLib := make([]byte, 4000)
			nLib, err := libEnc.Encode(frame, pktLib)
			if err != nil || nLib <= 0 {
				t.Fatalf("libopus Encode error: %v, n=%d", err, nLib)
			}
			if _, err := dec.Decode(pktLib[:nLib], pcmOut, 5760, false); err != nil {
				t.Fatalf("libopus roundtrip decode error: %v", err)
			}
		})
	}
}

func BenchmarkEncode_Matrix_GoOpusCodec(b *testing.B) {
	for _, cfg := range matrixConfigs {
		b.Run(cfg.name, func(b *testing.B) {
			enc, err := goopus.NewEncoder(cfg.sampleRate, cfg.channels, cfg.goApp)
			if err != nil {
				b.Fatalf("NewEncoder: %v", err)
			}
			defer enc.Close()
			_ = enc.SetBitrate(cfg.bitrate)
			_ = enc.SetComplexity(cfg.complexity)

			corpus := makePCMCorpus(cfg.sampleRate, cfg.channels, cfg.frameSize, 20)
			packet := make([]byte, 4000)
			// Warm up encoder's ccgo/TLS pseudostack before timing
			if len(corpus) > 0 {
				if _, err := enc.Encode(corpus[0], cfg.frameSize, packet); err != nil {
					b.Fatalf("Encode warm-up: %v", err)
				}
			}

			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				frame := corpus[i%len(corpus)]
				if _, err := enc.Encode(frame, cfg.frameSize, packet); err != nil {
					b.Fatalf("Encode: %v", err)
				}
			}
		})
	}
}

func BenchmarkEncode_Matrix_Libopus(b *testing.B) {
	for _, cfg := range matrixConfigs {
		b.Run(cfg.name, func(b *testing.B) {
			enc, err := hraban.NewEncoder(cfg.sampleRate, cfg.channels, cfg.libopusApp)
			if err != nil {
				b.Fatalf("NewEncoder: %v", err)
			}
			_ = enc.SetBitrate(cfg.bitrate)
			_ = enc.SetComplexity(cfg.complexity)

			corpus := makePCMCorpus(cfg.sampleRate, cfg.channels, cfg.frameSize, 20)
			packet := make([]byte, 4000)

			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				frame := corpus[i%len(corpus)]
				if _, err := enc.Encode(frame, packet); err != nil {
					b.Fatalf("Encode: %v", err)
				}
			}
		})
	}
}
