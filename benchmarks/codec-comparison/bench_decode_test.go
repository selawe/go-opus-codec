// Package benchcompare benchmarks this repository's pure-Go Opus decoder
// against two reference implementations decoding the exact same RFC 6716
// packets:
//
//   - github.com/hraban/opus: cgo bindings to the reference libopus C library.
//   - github.com/pion/opus:   an independent pure-Go Opus implementation.
//
// This is a separate Go module (mirroring the examples/* layout) so that the
// cgo dependency on libopus-dev never leaks into the main module's
// zero-dependency, CGO_ENABLED=0 build.
//
// Run with: go test -bench=. -benchmem
package benchcompare

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	hraban "github.com/hraban/opus"
	pion "github.com/pion/opus"
	goopus "github.com/selawe/go-opus-codec/opus"
)

const bitstreamDir = "../../testvectors"

// loadBitstreamPackets extracts real Opus packets from an RFC 6716 Section 6
// .bit test vector (see the main module's test/bitstream.go for the format:
// an 8-byte big-endian [payloadLen, wantFinalRange] header per frame).
func loadBitstreamPackets(path string, max int) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(data)
	var packets [][]byte
	for len(packets) < max {
		var header [8]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			break
		}
		payloadLen := binary.BigEndian.Uint32(header[0:4])
		if payloadLen == 0 {
			continue // packet-loss marker frame, no payload follows
		}
		if payloadLen > 65536 {
			break // desynced or corrupt, stop scanning
		}
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}
		packets = append(packets, payload)
	}
	return packets, nil
}

// loadAllBitstreamPackets extracts real Opus packets from all 12 RFC 6716
// Section 6 .bit test vectors (testvector01.bit..testvector12.bit) to create
// a diverse, representative corpus covering speech (SILK), music (CELT),
// hybrid mode, bandwidth transitions, and variable frame sizes.
func loadAllBitstreamPackets(dir string, maxPerVector int) ([][]byte, error) {
	var allPackets [][]byte
	for i := 1; i <= 12; i++ {
		path := fmt.Sprintf("%s/testvector%02d.bit", dir, i)
		pkts, err := loadBitstreamPackets(path, maxPerVector)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		allPackets = append(allPackets, pkts...)
	}
	return allPackets, nil
}

var corpus [][]byte

func init() {
	pkts, err := loadAllBitstreamPackets(bitstreamDir, 25)
	if err != nil {
		panic("benchcompare: failed to load " + bitstreamDir + ": " + err.Error())
	}
	if len(pkts) == 0 {
		panic("benchcompare: no packets extracted from " + bitstreamDir)
	}
	corpus = pkts
}

// TestDecodeSampleCountsAgree is a correctness sanity check that must pass
// before the benchmark numbers below are trusted: all three decoders must
// agree on how many samples per channel each packet decodes to.
func TestDecodeSampleCountsAgree(t *testing.T) {
	goDec, err := goopus.NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("go-opus-codec NewDecoder: %v", err)
	}
	defer goDec.Close()

	libDec, err := hraban.NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("hraban/opus NewDecoder: %v", err)
	}

	pionDec, err := pion.NewDecoderWithOutput(48000, 2)
	if err != nil {
		t.Fatalf("pion/opus NewDecoderWithOutput: %v", err)
	}

	goPCM := make([]int16, 5760*2)
	libPCM := make([]int16, 5760*2)
	pionPCM := make([]int16, 5760*2)

	for i, pkt := range corpus {
		goN, err := goDec.Decode(pkt, goPCM, 5760, false)
		if err != nil {
			t.Fatalf("packet %d: go-opus-codec Decode: %v", i, err)
		}
		libN, err := libDec.Decode(pkt, libPCM)
		if err != nil {
			t.Fatalf("packet %d: hraban/opus Decode: %v", i, err)
		}
		pionN, err := pionDec.DecodeToInt16(pkt, pionPCM)
		if err != nil {
			t.Fatalf("packet %d: pion/opus DecodeToInt16: %v", i, err)
		}
		if goN != libN || goN != pionN {
			t.Fatalf("packet %d: decoded sample-per-channel count mismatch: go-opus-codec=%d libopus=%d pion=%d", i, goN, libN, pionN)
		}
	}
}

func BenchmarkDecode_GoOpusCodec(b *testing.B) {
	dec, err := goopus.NewDecoder(48000, 2)
	if err != nil {
		b.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()
	pcm := make([]int16, 5760*2)
	// Warm up decoder's ccgo/TLS pseudostack and staging buffer before timing
	if len(corpus) > 0 {
		if _, err := dec.Decode(corpus[0], pcm, 5760, false); err != nil {
			b.Fatalf("Decode warm-up: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pkt := corpus[i%len(corpus)]
		if _, err := dec.Decode(pkt, pcm, 5760, false); err != nil {
			b.Fatalf("Decode: %v", err)
		}
	}
}

func BenchmarkDecode_Libopus(b *testing.B) {
	dec, err := hraban.NewDecoder(48000, 2)
	if err != nil {
		b.Fatalf("NewDecoder: %v", err)
	}
	pcm := make([]int16, 5760*2)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pkt := corpus[i%len(corpus)]
		if _, err := dec.Decode(pkt, pcm); err != nil {
			b.Fatalf("Decode: %v", err)
		}
	}
}

func BenchmarkDecode_PionOpus(b *testing.B) {
	dec, err := pion.NewDecoderWithOutput(48000, 2)
	if err != nil {
		b.Fatalf("NewDecoderWithOutput: %v", err)
	}
	pcm := make([]int16, 5760*2)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pkt := corpus[i%len(corpus)]
		if _, err := dec.DecodeToInt16(pkt, pcm); err != nil {
			b.Fatalf("DecodeToInt16: %v", err)
		}
	}
}

// ---- Phase 1: Real-World Matrix Decode Benchmarks ----

type matrixDecodeConfig struct {
	name       string
	sampleRate int
	channels   int
	frameSize  int
	bitrate    int
	app        int
}

var matrixDecodeConfigs = []matrixDecodeConfig{
	{"VoIP_16k_Mono_10ms", 16000, 1, 160, 24000, goopus.ApplicationVoIP},
	{"VoIP_16k_Mono_20ms", 16000, 1, 320, 24000, goopus.ApplicationVoIP},
	{"VoIP_16k_Mono_40ms", 16000, 1, 640, 24000, goopus.ApplicationVoIP},
	{"VoIP_16k_Mono_60ms", 16000, 1, 960, 24000, goopus.ApplicationVoIP},
	{"Audio_48k_Stereo_2.5ms", 48000, 2, 120, 64000, goopus.ApplicationAudio},
	{"Audio_48k_Stereo_5ms", 48000, 2, 240, 64000, goopus.ApplicationAudio},
	{"Audio_48k_Stereo_10ms", 48000, 2, 480, 64000, goopus.ApplicationAudio},
	{"Audio_48k_Stereo_20ms", 48000, 2, 960, 64000, goopus.ApplicationAudio},
	{"Audio_48k_Stereo_40ms", 48000, 2, 1920, 64000, goopus.ApplicationAudio},
	{"Audio_48k_Stereo_60ms", 48000, 2, 2880, 64000, goopus.ApplicationAudio},
}

func prepareMatrixDecodePackets(cfg matrixDecodeConfig, count int) ([][]byte, error) {
	enc, err := goopus.NewEncoder(cfg.sampleRate, cfg.channels, cfg.app)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	_ = enc.SetBitrate(cfg.bitrate)

	corpus := makePCMCorpus(cfg.sampleRate, cfg.channels, cfg.frameSize, count)
	var packets [][]byte
	for _, frame := range corpus {
		buf := make([]byte, 1275)
		n, err := enc.Encode(frame, cfg.frameSize, buf)
		if err != nil {
			return nil, err
		}
		packets = append(packets, buf[:n])
	}
	return packets, nil
}

func TestMatrixDecodeSampleCountsAgree(t *testing.T) {
	for _, cfg := range matrixDecodeConfigs {
		t.Run(cfg.name, func(t *testing.T) {
			pkts, err := prepareMatrixDecodePackets(cfg, 5)
			if err != nil {
				t.Fatalf("prepare packets: %v", err)
			}

			goDec, err := goopus.NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				t.Fatalf("goopus NewDecoder: %v", err)
			}
			defer goDec.Close()

			libDec, err := hraban.NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				t.Fatalf("hraban NewDecoder: %v", err)
			}

			pionDec, err := pion.NewDecoderWithOutput(cfg.sampleRate, cfg.channels)
			if err != nil {
				t.Fatalf("pion NewDecoderWithOutput: %v", err)
			}

			goPCM := make([]int16, 5760*cfg.channels)
			libPCM := make([]int16, 5760*cfg.channels)
			pionPCM := make([]int16, 5760*cfg.channels)

			for i, pkt := range pkts {
				goN, err := goDec.Decode(pkt, goPCM, 5760, false)
				if err != nil {
					t.Fatalf("packet %d goopus Decode: %v", i, err)
				}
				libN, err := libDec.Decode(pkt, libPCM)
				if err != nil {
					t.Fatalf("packet %d hraban Decode: %v", i, err)
				}
				pionN, err := pionDec.DecodeToInt16(pkt, pionPCM)
				if err != nil {
					t.Fatalf("packet %d pion DecodeToInt16: %v", i, err)
				}
				if goN != libN || goN != pionN {
					t.Fatalf("packet %d mismatch: go=%d lib=%d pion=%d", i, goN, libN, pionN)
				}
			}
		})
	}
}

func BenchmarkDecode_Matrix_GoOpusCodec(b *testing.B) {
	for _, cfg := range matrixDecodeConfigs {
		b.Run(cfg.name, func(b *testing.B) {
			pkts, err := prepareMatrixDecodePackets(cfg, 20)
			if err != nil {
				b.Fatalf("prepare packets: %v", err)
			}

			dec, err := goopus.NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				b.Fatalf("NewDecoder: %v", err)
			}
			defer dec.Close()

			pcm := make([]int16, 5760*cfg.channels)
			// Warm up decoder's ccgo/TLS pseudostack before timing
			if len(pkts) > 0 {
				if _, err := dec.Decode(pkts[0], pcm, 5760, false); err != nil {
					b.Fatalf("Decode warm-up: %v", err)
				}
			}

			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pkt := pkts[i%len(pkts)]
				if _, err := dec.Decode(pkt, pcm, 5760, false); err != nil {
					b.Fatalf("Decode: %v", err)
				}
			}
		})
	}
}

func BenchmarkDecode_Matrix_Libopus(b *testing.B) {
	for _, cfg := range matrixDecodeConfigs {
		b.Run(cfg.name, func(b *testing.B) {
			pkts, err := prepareMatrixDecodePackets(cfg, 20)
			if err != nil {
				b.Fatalf("prepare packets: %v", err)
			}

			dec, err := hraban.NewDecoder(cfg.sampleRate, cfg.channels)
			if err != nil {
				b.Fatalf("NewDecoder: %v", err)
			}

			pcm := make([]int16, 5760*cfg.channels)
			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pkt := pkts[i%len(pkts)]
				if _, err := dec.Decode(pkt, pcm); err != nil {
					b.Fatalf("Decode: %v", err)
				}
			}
		})
	}
}

func BenchmarkDecode_Matrix_PionOpus(b *testing.B) {
	for _, cfg := range matrixDecodeConfigs {
		b.Run(cfg.name, func(b *testing.B) {
			pkts, err := prepareMatrixDecodePackets(cfg, 20)
			if err != nil {
				b.Fatalf("prepare packets: %v", err)
			}

			dec, err := pion.NewDecoderWithOutput(cfg.sampleRate, cfg.channels)
			if err != nil {
				b.Fatalf("NewDecoderWithOutput: %v", err)
			}

			pcm := make([]int16, 5760*cfg.channels)
			b.SetBytes(int64(cfg.frameSize * cfg.channels * 2))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pkt := pkts[i%len(pkts)]
				if _, err := dec.DecodeToInt16(pkt, pcm); err != nil {
					b.Fatalf("DecodeToInt16: %v", err)
				}
			}
		})
	}
}

func TestPLC(t *testing.T) {
	// Test goopus
	goDec, err := goopus.NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("goopus: %v", err)
	}
	defer goDec.Close()
	pcm := make([]int16, 5760*2)
	nGo, err := goDec.Decode(nil, pcm, 960, false)
	if err != nil || nGo != 960 {
		t.Fatalf("goopus PLC failed: n=%d, err=%v", nGo, err)
	}

	// Note: hraban/opus returns "opus: no data supplied" on nil input,
	// and pion/opus returns "packet is too short to contain table of contents header".
	// Neither library currently supports RFC 6716 Section 3.4 PLC decoding.
}

// BenchmarkDecode_PLC_GoOpusCodec measures Packet Loss Concealment (PLC)
// throughput when reconstructing lost frames without bitstream input.
func BenchmarkDecode_PLC_GoOpusCodec(b *testing.B) {
	dec, err := goopus.NewDecoder(48000, 2)
	if err != nil {
		b.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	pcm := make([]int16, 5760*2)
	// Prime the decoder state with a real audio frame first
	if len(corpus) > 0 {
		_, _ = dec.Decode(corpus[0], pcm, 5760, false)
	}

	b.SetBytes(int64(960 * 2 * 2))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := dec.Decode(nil, pcm, 960, false); err != nil {
			b.Fatalf("Decode PLC: %v", err)
		}
	}
}
