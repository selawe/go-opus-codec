package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
)

// buildTestOgg synthesizes a minimal valid Ogg Opus file with numPackets
// mono audio packets and returns its path plus the pre-skip used.
func buildTestOgg(t *testing.T, numPackets int) (path string, preSkip int) {
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
	pw := ogg.NewPacketWriter(&buf, 0x11223344)

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
	tagsPkt, err := ogg.BuildOpusTagsPacket(ogg.OpusTags{Vendor: "test-vendor", Comments: []string{"A=1", "B=2"}})
	if err != nil {
		t.Fatalf("BuildOpusTagsPacket: %v", err)
	}
	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		t.Fatalf("write head: %v", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		t.Fatalf("write tags: %v", err)
	}
	// Force the header packets onto their own page so audio packets start a
	// fresh page with its own checksum (needed by tests that corrupt only
	// the audio page's CRC without touching the mandatory header page).
	if err := pw.FlushPage(); err != nil {
		t.Fatalf("flush header page: %v", err)
	}

	pcm := make([]int16, frameSize)
	packetBuf := make([]byte, 2000)
	var total uint64
	for i := 0; i < numPackets; i++ {
		for j := range pcm {
			pcm[j] = int16((j + i*7) % 1000)
		}
		n, err := enc.Encode(pcm, frameSize, packetBuf)
		if err != nil {
			t.Fatalf("encode frame %d: %v", i, err)
		}
		total += frameSize
		granule := uint64(lookahead) + total
		last := i == numPackets-1
		if err := pw.WritePacket(append([]byte(nil), packetBuf[:n]...), granule, false, last); err != nil {
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
	return path, lookahead
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

func TestOggOpusDump_HeaderAndPacketOutput(t *testing.T) {
	oggPath, preSkip := buildTestOgg(t, 3)

	var stdout, stderr bytes.Buffer
	code := run([]string{oggPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "OpusHead: version=1 channels=1 preSkip="+strconv.Itoa(preSkip)+" inputRate=48000 mapping=0") {
		t.Fatalf("missing/incorrect OpusHead line in output:\n%s", out)
	}
	if !strings.Contains(out, `OpusTags: vendor="test-vendor" comments=2`) {
		t.Fatalf("missing/incorrect OpusTags line in output:\n%s", out)
	}
	if !strings.Contains(out, "Preskip samples: "+strconv.Itoa(preSkip)) {
		t.Fatalf("missing preskip samples line in output:\n%s", out)
	}

	pktLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "pkt=") {
			pktLines++
			isLast := pktLines == 3
			if isLast && !strings.Contains(line, "eos=true") {
				t.Fatalf("expected last packet line to have eos=true, got: %s", line)
			}
			if !isLast && !strings.Contains(line, "eos=false") {
				t.Fatalf("expected non-last packet line to have eos=false, got: %s", line)
			}
		}
	}
	if pktLines != 3 {
		t.Fatalf("expected 3 pkt lines, got %d:\n%s", pktLines, out)
	}
}

func TestOggOpusDump_MaxFlag(t *testing.T) {
	oggPath, _ := buildTestOgg(t, 5)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--max", "2", oggPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}

	pktLines := 0
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, "pkt=") {
			pktLines++
		}
	}
	if pktLines != 2 {
		t.Fatalf("expected 2 pkt lines with --max 2, got %d:\n%s", pktLines, stdout.String())
	}
}

func TestOggOpusDump_NoCRCFlag(t *testing.T) {
	// buildTestOgg forces OpusHead/OpusTags onto their own page (FlushPage)
	// and each audio packet ends up on its own page too, so a fixture with 3
	// audio packets has 5 pages total: head, tags, then one page per packet.
	oggPath, _ := buildTestOgg(t, 3)

	raw, err := os.ReadFile(oggPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Corrupt the checksum field (bytes 22-25 relative to page start) of the
	// first AUDIO page, not the OpusHead/OpusTags header pages: NewOpusReader
	// always verifies CRC while parsing the mandatory header pages regardless
	// of --no-crc (SetVerifyCRC only takes effect for audio packets read
	// afterward), so the third "OggS" occurrence (index 2) is the first
	// audio page.
	pageOffsets := findOggPageOffsets(raw)
	if len(pageOffsets) < 3 {
		t.Fatalf("expected at least 3 Ogg pages in fixture, found %d", len(pageOffsets))
	}
	audioPageOffset := pageOffsets[2]
	corrupted := append([]byte(nil), raw...)
	corrupted[audioPageOffset+22] ^= 0xFF

	corruptPath := filepath.Join(t.TempDir(), "corrupt.opus")
	if err := os.WriteFile(corruptPath, corrupted, 0o600); err != nil {
		t.Fatalf("write corrupted fixture: %v", err)
	}

	// With CRC verification on (default), PageReader's built-in resync (RFC
	// 3533 recovery) silently scans past the corrupted page and resumes at
	// the next valid one, rather than returning a fatal error - so the
	// observable effect is a dropped packet (2 pkt lines instead of 3), not
	// a non-zero exit code.
	var stdout, stderr bytes.Buffer
	code := run([]string{corruptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 (corrupted page recovered via resync), got %d (stderr=%q)", code, stderr.String())
	}
	if got := countPktLines(stdout.String()); got != 2 {
		t.Fatalf("expected 2 pkt lines with CRC verification dropping the corrupted page, got %d:\n%s", got, stdout.String())
	}

	// With --no-crc, the corrupted page's checksum is never inspected, so
	// its packet (whose payload bytes are untouched - only the checksum
	// field was flipped) survives and all 3 packets are reported.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--no-crc", corruptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 with --no-crc on corrupted CRC, got %d (stderr=%q)", code, stderr.String())
	}
	if got := countPktLines(stdout.String()); got != 3 {
		t.Fatalf("expected 3 pkt lines with --no-crc bypassing checksum verification, got %d:\n%s", got, stdout.String())
	}
}

func countPktLines(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "pkt=") {
			n++
		}
	}
	return n
}

func TestOggOpusDump_UsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2 for missing args, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("expected stderr to contain usage message, got: %s", stderr.String())
	}
}

func TestOggOpusDump_FileNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{filepath.Join(t.TempDir(), "does-not-exist.opus")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing file, got %d", code)
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("expected stderr to contain error message, got: %s", stderr.String())
	}
}
