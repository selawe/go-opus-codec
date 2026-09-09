package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
)

// buildTestOgg synthesizes a minimal valid Ogg Opus file with numPackets
// mono audio packets, each on its own page (OpusHead/OpusTags forced onto
// their own leading page). It returns the file path and the original
// (uncompressed-container) packet payloads in encode order, for byte-exact
// round-trip comparison.
func buildTestOgg(t *testing.T, numPackets int) (path string, packets [][]byte) {
	t.Helper()

	const sampleRate = 48000
	const channels = 1
	const frameSize = 960

	enc, err := opus.NewEncoder(sampleRate, channels, opus.ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	lookahead, err := enc.Lookahead()
	if err != nil {
		t.Fatalf("Lookahead: %v", err)
	}

	var buf bytes.Buffer
	pw := ogg.NewPacketWriter(&buf, 0x22334455)

	head := ogg.OpusHead{
		Version:              1,
		Channels:             channels,
		PreSkip:              uint16(lookahead),
		InputSampleRate:      sampleRate,
		ChannelMappingFamily: 0,
	}
	headPkt, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		t.Fatalf("BuildOpusHeadPacket: %v", err)
	}
	tagsPkt, err := ogg.BuildOpusTagsPacket(ogg.OpusTags{Vendor: "test-vendor"})
	if err != nil {
		t.Fatalf("BuildOpusTagsPacket: %v", err)
	}
	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		t.Fatalf("write head: %v", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		t.Fatalf("write tags: %v", err)
	}
	if err := pw.FlushPage(); err != nil {
		t.Fatalf("flush header page: %v", err)
	}

	pcm := make([]int16, frameSize)
	packetBuf := make([]byte, 2000)
	var total uint64
	for i := 0; i < numPackets; i++ {
		for j := range pcm {
			pcm[j] = int16((j + i*11) % 1000)
		}
		n, err := enc.Encode(pcm, frameSize, packetBuf)
		if err != nil {
			t.Fatalf("encode frame %d: %v", i, err)
		}
		payload := append([]byte(nil), packetBuf[:n]...)
		packets = append(packets, payload)

		total += frameSize
		granule := uint64(lookahead) + total
		last := i == numPackets-1
		if err := pw.WritePacket(payload, granule, false, last); err != nil {
			t.Fatalf("write packet %d: %v", i, err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	dir := t.TempDir()
	path = filepath.Join(dir, "test.opus")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write ogg file: %v", err)
	}
	return path, packets
}

// findOggPageOffsets returns the byte offset of every "OggS" page capture
// pattern in raw, in order.
func findOggPageOffsets(raw []byte) []int {
	var offsets []int
	idx := 0
	for {
		i := bytes.Index(raw[idx:], []byte("OggS"))
		if i < 0 {
			break
		}
		pos := idx + i
		offsets = append(offsets, pos)
		idx = pos + 4
	}
	return offsets
}

// parseLengthPrefixed decodes the extractor's u32le-length-prefixed packet
// stream format back into individual packet payloads.
func parseLengthPrefixed(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for len(data) > 0 {
		if len(data) < 4 {
			t.Fatalf("truncated length prefix, %d bytes remaining", len(data))
		}
		n := binary.LittleEndian.Uint32(data[:4])
		data = data[4:]
		if uint32(len(data)) < n {
			t.Fatalf("truncated packet body: need %d bytes, have %d", n, len(data))
		}
		out = append(out, append([]byte(nil), data[:n]...))
		data = data[n:]
	}
	return out
}

func assertPacketsEqual(t *testing.T, want, got [][]byte) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("expected %d packets, got %d", len(want), len(got))
	}
	for i := range want {
		if !bytes.Equal(want[i], got[i]) {
			t.Fatalf("packet %d mismatch: want %d bytes, got %d bytes", i, len(want[i]), len(got[i]))
		}
	}
}

func TestOggOpusExtract_RoundTrip(t *testing.T) {
	oggPath, wantPackets := buildTestOgg(t, 4)
	outPath := filepath.Join(t.TempDir(), "packets.bin")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, oggPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	gotPackets := parseLengthPrefixed(t, raw)
	assertPacketsEqual(t, wantPackets, gotPackets)
}

func TestOggOpusExtract_StdoutDefault(t *testing.T) {
	oggPath, wantPackets := buildTestOgg(t, 2)

	var stdout, stderr bytes.Buffer
	code := run([]string{oggPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}

	gotPackets := parseLengthPrefixed(t, stdout.Bytes())
	assertPacketsEqual(t, wantPackets, gotPackets)
}

func TestOggOpusExtract_NoCRCFlag(t *testing.T) {
	oggPath, wantPackets := buildTestOgg(t, 3)

	raw, err := os.ReadFile(oggPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	pageOffsets := findOggPageOffsets(raw)
	if len(pageOffsets) < 3 {
		t.Fatalf("expected at least 3 Ogg pages in fixture, found %d", len(pageOffsets))
	}
	// Corrupt the checksum of the first audio page (third page overall,
	// after the header and tags pages).
	corrupted := append([]byte(nil), raw...)
	corrupted[pageOffsets[2]+22] ^= 0xFF

	corruptPath := filepath.Join(t.TempDir(), "corrupt.opus")
	if err := os.WriteFile(corruptPath, corrupted, 0o600); err != nil {
		t.Fatalf("write corrupted fixture: %v", err)
	}

	// Default CRC verification resyncs past the corrupted page, dropping
	// its packet.
	var stdout, stderr bytes.Buffer
	code := run([]string{corruptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	gotPackets := parseLengthPrefixed(t, stdout.Bytes())
	if len(gotPackets) != len(wantPackets)-1 {
		t.Fatalf("expected %d packets (one dropped by resync), got %d", len(wantPackets)-1, len(gotPackets))
	}

	// --no-crc bypasses the checksum, so the packet's untouched payload
	// bytes survive.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--no-crc", corruptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 with --no-crc, got %d (stderr=%q)", code, stderr.String())
	}
	gotPackets = parseLengthPrefixed(t, stdout.Bytes())
	assertPacketsEqual(t, wantPackets, gotPackets)
}

func TestOggOpusExtract_UsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2 for missing args, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("expected stderr to contain usage message, got: %s", stderr.String())
	}
}

func TestOggOpusExtract_FileNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{filepath.Join(t.TempDir(), "does-not-exist.opus")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing file, got %d", code)
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("expected stderr to contain error message, got: %s", stderr.String())
	}
}
