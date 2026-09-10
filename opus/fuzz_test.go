package opus

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// addBitstreamSeeds best-effort loads real Opus packets out of an RFC 6716 Section 6
// .bit test vector (see test/bitstream.go for the format) and adds them as fuzz seeds.
// Missing files are silently skipped so the fuzz targets still work without the
// (large, separately downloaded) testvectors/ directory present.
func addBitstreamSeeds(f *testing.F, path string, maxPackets int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	r := bytes.NewReader(data)
	for range maxPackets {
		var header [8]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return
		}
		payloadLen := binary.BigEndian.Uint32(header[0:4])
		if payloadLen == 0 {
			continue // packet-loss marker frame, no payload follows
		}
		if payloadLen > 65536 {
			return // desynced or corrupt, stop scanning
		}
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return
		}
		f.Add(payload)
	}
}

func seedPacketCorpus(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})       // code 0, zero-length frame
	f.Add([]byte{0xFC, 0x00}) // code 3, frame count 0 (invalid)
	f.Add([]byte{0xFF, 0xFF}) // code 3, frame count 63 (invalid, > 48)

	root := filepath.Join("..", "testvectors")
	addBitstreamSeeds(f, filepath.Join(root, "testvector01.bit"), 20)
	addBitstreamSeeds(f, filepath.Join(root, "testvector03.bit"), 20)
}

// FuzzPacketFrames exercises the hand-written RFC 6716 Section 3.2 frame demuxer
// directly (no libopus decode involved), checking it never reports more frame
// bytes than were present in the input packet.
func FuzzPacketFrames(f *testing.F) {
	seedPacketCorpus(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		frames, err := PacketFrames(data)
		if err != nil {
			return
		}
		total := 0
		for _, fr := range frames {
			total += len(fr)
		}
		if total > len(data) {
			t.Fatalf("PacketFrames reported %d total frame bytes from a %d-byte packet", total, len(data))
		}
	})
}

// FuzzPacketTotalSamples exercises PacketFrameCount/PacketSamplesPerFrame/PacketTotalSamples
// on arbitrary TOC bytes.
func FuzzPacketTotalSamples(f *testing.F) {
	seedPacketCorpus(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = PacketTotalSamples(data, 48000)
	})
}

// FuzzDecode feeds arbitrary bytes to the libopus-backed int16 decoder. It only
// checks that decoding never panics; malformed packets are expected to return
// an error, which is not itself a failure.
func FuzzDecode(f *testing.F) {
	seedPacketCorpus(f)

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		f.Fatalf("NewDecoder: %v", err)
	}
	f.Cleanup(func() { _ = dec.Close() })

	pcm := make([]int16, MaxFrameSize*2*2)
	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := dec.Decode(data, pcm, 5760, false); err != nil {
			return
		}
		if _, err := dec.Decode(data, pcm, 5760, true); err != nil {
			return
		}
	})
}

// FuzzDecodeF32 is the float32 counterpart of FuzzDecode.
func FuzzDecodeF32(f *testing.F) {
	seedPacketCorpus(f)

	dec, err := NewDecoder(48000, 2)
	if err != nil {
		f.Fatalf("NewDecoder: %v", err)
	}
	f.Cleanup(func() { _ = dec.Close() })

	pcm := make([]float32, MaxFrameSize*2*2)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = dec.DecodeF32(data, pcm, 5760, false)
	})
}

// FuzzPacketUnpad exercises the RFC 8251 CBR unpad path, which parses and
// rewrites the packet's frame-length header in place.
func FuzzPacketUnpad(f *testing.F) {
	seedPacketCorpus(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		in := append([]byte(nil), data...)
		out, err := PacketUnpad(in)
		if err != nil {
			return
		}
		if len(out) > len(data) {
			t.Fatalf("PacketUnpad grew a %d-byte packet to %d bytes", len(data), len(out))
		}
	})
}
