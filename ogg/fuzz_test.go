package ogg

import (
	"bytes"
	"os"
	"testing"
)

// FuzzOpusReader feeds arbitrary bytes into the Ogg Opus container demuxer
// (RFC 3533 page parsing + RFC 7845 header parsing) and drains it as an
// io.Reader-based consumer would. It only checks that no input can crash the
// reader or send it into an unbounded loop; errors from malformed input are
// expected and not themselves a failure.
func FuzzOpusReader(f *testing.F) {
	f.Add([]byte("OggS"))
	if data, err := os.ReadFile("../test/music_64kbps.opus"); err == nil {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := NewOpusReader(bytes.NewReader(data))
		if err != nil {
			return
		}

		const maxPackets = 100000
		for range maxPackets {
			if _, err := r.ReadAudioPacket(); err != nil {
				return
			}
		}
		t.Fatalf("ReadAudioPacket did not terminate within %d packets", maxPackets)
	})
}

// FuzzPageReader tests RFC 3533 PageReader against arbitrary input bytes.
func FuzzPageReader(f *testing.F) {
	f.Add([]byte("OggS"))
	if data, err := os.ReadFile("../test/music_64kbps.opus"); err == nil {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		pr := NewPageReader(bytes.NewReader(data))
		const maxPages = 10000
		for range maxPages {
			if _, err := pr.ReadPage(); err != nil {
				return
			}
		}
	})
}

// FuzzPacketReader tests PacketReader packet reconstruction against arbitrary byte streams.
func FuzzPacketReader(f *testing.F) {
	f.Add([]byte("OggS"))
	if data, err := os.ReadFile("../test/music_64kbps.opus"); err == nil {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		pkr := NewPacketReader(bytes.NewReader(data))
		const maxPackets = 10000
		for range maxPackets {
			if _, err := pkr.ReadPacket(); err != nil {
				return
			}
		}
	})
}

// With CRC checking on, a mutated input is almost always rejected at the first page, so
// the lacing, continuation, header and tag logic behind it is barely exercised. These
// targets switch verification off so mutations reach that code.

// FuzzOpusReaderNoCRC drains the demuxer with checksum verification disabled.
func FuzzOpusReaderNoCRC(f *testing.F) {
	f.Add([]byte("OggS"))
	// A two-stream chain, so mutations exercise the stream-boundary handling.
	if a, err := buildMockOpusStream(1, 2, 312, "a", [][]byte{{0xFC, 1}, {0xFC, 2}}); err == nil {
		if b, err := buildMockOpusStream(2, 2, 312, "b", [][]byte{{0xFC, 3}}); err == nil {
			f.Add(append(a, b...))
		}
	}
	if data, err := os.ReadFile("../test/music_64kbps.opus"); err == nil {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := NewOpusReaderVerifyCRC(bytes.NewReader(data), false)
		if err != nil {
			return
		}
		const maxPackets = 100000
		for range maxPackets {
			if _, err := r.ReadAudioPacket(); err != nil {
				return
			}
		}
		t.Fatalf("ReadAudioPacket did not terminate within %d packets", maxPackets)
	})
}

// FuzzOpusHeaders feeds arbitrary bytes straight to the OpusHead and OpusTags parsers and
// checks that whatever parses also survives a build/parse round trip.
func FuzzOpusHeaders(f *testing.F) {
	f.Add([]byte("OpusHead\x01\x02\x38\x01\x80\xbb\x00\x00\x00\x00\x00"))
	f.Add([]byte("OpusTags\x04\x00\x00\x00test\x01\x00\x00\x00\x03\x00\x00\x00A=B"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if h, err := parseOpusHead(data); err == nil {
			if pkt, err := BuildOpusHeadPacket(h); err == nil {
				if _, err := parseOpusHead(pkt); err != nil {
					t.Fatalf("rebuilt OpusHead does not parse: %v", err)
				}
			}
		}
		if tags, err := parseOpusTags(data); err == nil {
			pkt, err := BuildOpusTagsPacket(tags)
			if err != nil {
				t.Fatalf("BuildOpusTagsPacket: %v", err)
			}
			back, err := parseOpusTags(pkt)
			if err != nil || back.Vendor != tags.Vendor && tags.Vendor != "" || len(back.Comments) != len(tags.Comments) {
				t.Fatalf("tags round trip changed: %+v -> %+v (%v)", tags, back, err)
			}
		}
	})
}

// FuzzSeekAndLength exercises the seeking and length paths, which do their own page probing.
func FuzzSeekAndLength(f *testing.F) {
	// A prefix of the sample keeps the corpus entry small enough for the fuzzer to mutate quickly.
	if data, err := os.ReadFile("../test/music_64kbps.opus"); err == nil {
		f.Add(data[:min(len(data), 32<<10)], uint64(48000))
	}

	f.Fuzz(func(t *testing.T, data []byte, target uint64) {
		r, err := NewOpusReaderVerifyCRC(bytes.NewReader(data), false)
		if err != nil {
			return
		}
		_, _ = r.TotalSamples()
		_, _ = r.SeekToPage(target)
		for range 1000 {
			if _, err := r.ReadAudioPacket(); err != nil {
				return
			}
		}
	})
}
