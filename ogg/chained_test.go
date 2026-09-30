package ogg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

func buildMockOpusStream(serial uint32, channels uint8, preSkip uint16, artist string, audioPackets [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, serial)

	head := OpusHead{
		Version:              1,
		Channels:             channels,
		PreSkip:              preSkip,
		InputSampleRate:      48000,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	headPkt, err := BuildOpusHeadPacket(head)
	if err != nil {
		return nil, fmt.Errorf("build head: %w", err)
	}
	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		return nil, fmt.Errorf("write head: %w", err)
	}
	if err := pw.Flush(); err != nil {
		return nil, fmt.Errorf("flush head: %w", err)
	}

	tagsPkt, err := BuildOpusTagsPacket(OpusTags{
		Vendor:   "opusgo-test",
		Comments: []string{"ARTIST=" + artist},
	})
	if err != nil {
		return nil, fmt.Errorf("build tags: %w", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		return nil, fmt.Errorf("write tags: %w", err)
	}
	if err := pw.Flush(); err != nil {
		return nil, fmt.Errorf("flush tags: %w", err)
	}

	granule := uint64(preSkip)
	for i, ap := range audioPackets {
		isLast := (i == len(audioPackets)-1)
		granule += 960
		if err := pw.WritePacket(ap, granule, false, isLast); err != nil {
			return nil, fmt.Errorf("write audio %d: %w", i, err)
		}
		if isLast {
			if err := pw.Flush(); err != nil {
				return nil, fmt.Errorf("flush last audio: %w", err)
			}
		}
	}

	return buf.Bytes(), nil
}

func TestPacketReader_ChainedStreams(t *testing.T) {
	var stream1 bytes.Buffer
	pw1 := NewPacketWriter(&stream1, 0x11111111)
	if err := pw1.WritePacket([]byte("s1-bos"), 0, true, false); err != nil {
		t.Fatalf("pw1 write bos: %v", err)
	}
	if err := pw1.Flush(); err != nil {
		t.Fatalf("pw1 flush: %v", err)
	}
	if err := pw1.WritePacket([]byte("s1-p1"), 960, false, false); err != nil {
		t.Fatalf("pw1 write p1: %v", err)
	}
	if err := pw1.WritePacket([]byte("s1-p2"), 1920, false, true); err != nil {
		t.Fatalf("pw1 write p2: %v", err)
	}
	if err := pw1.Flush(); err != nil {
		t.Fatalf("pw1 flush: %v", err)
	}

	var stream2 bytes.Buffer
	pw2 := NewPacketWriter(&stream2, 0x22222222)
	if err := pw2.WritePacket([]byte("s2-bos"), 0, true, false); err != nil {
		t.Fatalf("pw2 write bos: %v", err)
	}
	if err := pw2.Flush(); err != nil {
		t.Fatalf("pw2 flush: %v", err)
	}
	if err := pw2.WritePacket([]byte("s2-p1"), 960, false, true); err != nil {
		t.Fatalf("pw2 write p1: %v", err)
	}
	if err := pw2.Flush(); err != nil {
		t.Fatalf("pw2 flush: %v", err)
	}

	chained := append(stream1.Bytes(), stream2.Bytes()...)
	pr := NewPacketReader(bytes.NewReader(chained))

	// Stream 1
	p1, err := pr.ReadPacket()
	if err != nil || string(p1.Data) != "s1-bos" || p1.Serial != 0x11111111 || !p1.BOS || pr.StreamIndex() != 0 {
		t.Fatalf("unexpected p1: err=%v, data=%s, serial=%x, bos=%v, idx=%d", err, string(p1.Data), p1.Serial, p1.BOS, pr.StreamIndex())
	}
	p2, err := pr.ReadPacket()
	if err != nil || string(p2.Data) != "s1-p1" || p2.Serial != 0x11111111 || pr.StreamIndex() != 0 {
		t.Fatalf("unexpected p2: err=%v, data=%s", err, string(p2.Data))
	}
	p3, err := pr.ReadPacket()
	if err != nil || string(p3.Data) != "s1-p2" || !p3.EOS || p3.Serial != 0x11111111 || pr.StreamIndex() != 0 {
		t.Fatalf("unexpected p3: err=%v, eos=%v", err, p3.EOS)
	}

	// Transition to Stream 2
	p4, err := pr.ReadPacket()
	if err != nil || string(p4.Data) != "s2-bos" || p4.Serial != 0x22222222 || !p4.BOS || pr.StreamIndex() != 1 {
		t.Fatalf("unexpected p4: err=%v, data=%s, serial=%x, bos=%v, idx=%d", err, string(p4.Data), p4.Serial, p4.BOS, pr.StreamIndex())
	}
	p5, err := pr.ReadPacket()
	if err != nil || string(p5.Data) != "s2-p1" || !p5.EOS || p5.Serial != 0x22222222 || pr.StreamIndex() != 1 {
		t.Fatalf("unexpected p5: err=%v, eos=%v", err, p5.EOS)
	}

	// End of chained streams
	_, err = pr.ReadPacket()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF at end of chained streams, got %v", err)
	}
}

func TestPacketReader_ChainedStream_MissingBOS(t *testing.T) {
	var stream1 bytes.Buffer
	pw1 := NewPacketWriter(&stream1, 0x11111111)
	_ = pw1.WritePacket([]byte("s1-bos"), 0, true, true) // BOS + EOS
	_ = pw1.Flush()

	// Stream 2 starts WITHOUT BOS flag
	var stream2 bytes.Buffer
	pw2 := NewPacketWriter(&stream2, 0x22222222)
	_ = pw2.WritePacket([]byte("s2-not-bos"), 960, false, true)
	_ = pw2.Flush()

	chained := append(stream1.Bytes(), stream2.Bytes()...)
	pr := NewPacketReader(bytes.NewReader(chained))

	p1, err := pr.ReadPacket()
	if err != nil || !p1.EOS {
		t.Fatalf("read stream 1 failed: %v", err)
	}

	// Next page should fail because streamEnded is true but page has no BOS
	_, err = pr.ReadPacket()
	if !errors.Is(err, ErrSerialMismatch) {
		t.Fatalf("expected ErrSerialMismatch for chained page missing BOS, got %v", err)
	}
}

func TestPacketReader_SerialMismatchWithoutEOS(t *testing.T) {
	var stream1 bytes.Buffer
	pw1 := NewPacketWriter(&stream1, 0x11111111)
	_ = pw1.WritePacket([]byte("s1-bos"), 0, true, false) // no EOS
	_ = pw1.Flush()

	var stream2 bytes.Buffer
	pw2 := NewPacketWriter(&stream2, 0x22222222)
	_ = pw2.WritePacket([]byte("s2-bos"), 0, true, true)
	_ = pw2.Flush()

	chained := append(stream1.Bytes(), stream2.Bytes()...)
	pr := NewPacketReader(bytes.NewReader(chained))

	_, err := pr.ReadPacket()
	if err != nil {
		t.Fatalf("read p1: %v", err)
	}

	// Next page switches serial without prior EOS
	_, err = pr.ReadPacket()
	if !errors.Is(err, ErrSerialMismatch) {
		t.Fatalf("expected ErrSerialMismatch for unclosed stream serial switch, got %v", err)
	}
}

func TestOpusReader_ChainedStreams(t *testing.T) {
	// Valid TOC 0xFC (stereo 20ms)
	pktAudio := []byte{0xFC, 0x01, 0x02, 0x03}

	s1, err := buildMockOpusStream(0x1111, 2, 312, "Alice", [][]byte{pktAudio, pktAudio})
	if err != nil {
		t.Fatalf("build s1: %v", err)
	}

	s2, err := buildMockOpusStream(0x2222, 2, 120, "Bob", [][]byte{pktAudio, pktAudio, pktAudio})
	if err != nil {
		t.Fatalf("build s2: %v", err)
	}

	chained := append(s1, s2...)
	r, err := NewOpusReader(bytes.NewReader(chained))
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}

	// Check initial stream 0 headers
	if r.StreamIndex() != 0 {
		t.Fatalf("expected stream index 0, got %d", r.StreamIndex())
	}
	if r.Head.PreSkip != 312 {
		t.Fatalf("expected PreSkip 312, got %d", r.Head.PreSkip)
	}
	if r.Tags.Get("ARTIST") != "Alice" {
		t.Fatalf("expected ARTIST Alice, got %s", r.Tags.Get("ARTIST"))
	}

	// Read audio packet 1 (stream 0)
	p1, err := r.ReadAudioPacket()
	if err != nil || p1.NewStream || p1.StreamIndex != 0 || p1.EOS {
		t.Fatalf("unexpected p1: err=%v, newStream=%v, idx=%d, eos=%v", err, p1.NewStream, p1.StreamIndex, p1.EOS)
	}

	// Read audio packet 2 (stream 0 EOS)
	p2, err := r.ReadAudioPacket()
	if err != nil || p2.NewStream || p2.StreamIndex != 0 || !p2.EOS {
		t.Fatalf("unexpected p2: err=%v, newStream=%v, idx=%d, eos=%v", err, p2.NewStream, p2.StreamIndex, p2.EOS)
	}

	// Read audio packet 3 -> transition to stream 1!
	p3, err := r.ReadAudioPacket()
	if err != nil {
		t.Fatalf("read p3 (stream 1 start): %v", err)
	}
	if !p3.NewStream {
		t.Fatalf("expected NewStream true for p3, got false")
	}
	if p3.StreamIndex != 1 || r.StreamIndex() != 1 {
		t.Fatalf("expected StreamIndex 1, got pkt=%d, reader=%d", p3.StreamIndex, r.StreamIndex())
	}
	if r.Head.PreSkip != 120 {
		t.Fatalf("expected updated PreSkip 120, got %d", r.Head.PreSkip)
	}
	if r.Tags.Get("ARTIST") != "Bob" {
		t.Fatalf("expected updated ARTIST Bob, got %s", r.Tags.Get("ARTIST"))
	}
	if p3.EOS {
		t.Fatalf("expected p3 not EOS")
	}

	// Read audio packet 4 (stream 1)
	p4, err := r.ReadAudioPacket()
	if err != nil || p4.NewStream || p4.StreamIndex != 1 || p4.EOS {
		t.Fatalf("unexpected p4: err=%v, newStream=%v, idx=%d, eos=%v", err, p4.NewStream, p4.StreamIndex, p4.EOS)
	}

	// Read audio packet 5 (stream 1 EOS)
	p5, err := r.ReadAudioPacket()
	if err != nil || p5.NewStream || p5.StreamIndex != 1 || !p5.EOS {
		t.Fatalf("unexpected p5: err=%v, newStream=%v, idx=%d, eos=%v", err, p5.NewStream, p5.StreamIndex, p5.EOS)
	}

	// Read packet 6 -> EOF
	_, err = r.ReadAudioPacket()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF at end of chained reader, got %v", err)
	}
}
