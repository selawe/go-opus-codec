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
	"io"
	"os"
	"testing"

	hraban "github.com/hraban/opus"
	pion "github.com/pion/opus"
	goopus "github.com/selawe/go-opus-codec/opus"
)

const bitstreamPath = "../../testvectors/testvector01.bit"

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

var corpus [][]byte

func init() {
	pkts, err := loadBitstreamPackets(bitstreamPath, 200)
	if err != nil {
		panic("benchcompare: failed to load " + bitstreamPath + ": " + err.Error())
	}
	if len(pkts) == 0 {
		panic("benchcompare: no packets extracted from " + bitstreamPath)
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
