package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestRFC7845_OpusHeadFamily0_Stereo(t *testing.T) {
	head := OpusHead{
		Version:              1,
		Channels:             2,
		PreSkip:              312,
		InputSampleRate:      48000,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}

	pkt, err := BuildOpusHeadPacket(head)
	if err != nil {
		t.Fatalf("BuildOpusHeadPacket: %v", err)
	}

	if len(pkt) != 19 {
		t.Fatalf("expected 19 bytes for family 0, got %d", len(pkt))
	}

	parsed, err := parseOpusHead(pkt)
	if err != nil {
		t.Fatalf("parseOpusHead: %v", err)
	}

	if parsed.Version != 1 {
		t.Fatalf("expected version 1, got %d", parsed.Version)
	}
	if parsed.Channels != 2 {
		t.Fatalf("expected channels 2, got %d", parsed.Channels)
	}
	if parsed.PreSkip != 312 {
		t.Fatalf("expected preskip 312, got %d", parsed.PreSkip)
	}
	if parsed.InputSampleRate != 48000 {
		t.Fatalf("expected sample rate 48000, got %d", parsed.InputSampleRate)
	}
	if parsed.OutputGainQ8 != 0 {
		t.Fatalf("expected output gain 0, got %d", parsed.OutputGainQ8)
	}
	if parsed.ChannelMappingFamily != 0 {
		t.Fatalf("expected family 0, got %d", parsed.ChannelMappingFamily)
	}
}

func TestRFC7845_OpusHeadFamily1_Surround(t *testing.T) {
	// 5.1 surround sound: 6 channels, 4 streams, 2 coupled streams
	// Vorbis channel mapping: FL, C, FR, RL, RR, LFE
	mapping := []uint8{0, 4, 1, 2, 3, 5}
	head := OpusHead{
		Version:              1,
		Channels:             6,
		PreSkip:              312,
		InputSampleRate:      48000,
		OutputGainQ8:         256, // +1 dB
		ChannelMappingFamily: 1,
		StreamCount:          4,
		CoupledStreamCount:   2,
		ChannelMapping:       mapping,
	}

	pkt, err := BuildOpusHeadPacket(head)
	if err != nil {
		t.Fatalf("BuildOpusHeadPacket: %v", err)
	}

	expectedLen := 21 + len(mapping)
	if len(pkt) != expectedLen {
		t.Fatalf("expected %d bytes for family 1, got %d", expectedLen, len(pkt))
	}

	parsed, err := parseOpusHead(pkt)
	if err != nil {
		t.Fatalf("parseOpusHead: %v", err)
	}

	if parsed.Channels != 6 {
		t.Fatalf("expected 6 channels, got %d", parsed.Channels)
	}
	if parsed.StreamCount != 4 {
		t.Fatalf("expected 4 streams, got %d", parsed.StreamCount)
	}
	if parsed.CoupledStreamCount != 2 {
		t.Fatalf("expected 2 coupled streams, got %d", parsed.CoupledStreamCount)
	}
	if parsed.OutputGainQ8 != 256 {
		t.Fatalf("expected gain 256, got %d", parsed.OutputGainQ8)
	}
	if !bytes.Equal(parsed.ChannelMapping, mapping) {
		t.Fatalf("mapping mismatch: got %v, want %v", parsed.ChannelMapping, mapping)
	}
}

func TestRFC7845_OpusHeadValidationErrors(t *testing.T) {
	// Channels = 0 must be rejected
	_, err := BuildOpusHeadPacket(OpusHead{Channels: 0, ChannelMappingFamily: 0})
	if err == nil {
		t.Fatal("expected error for Channels=0, got nil")
	}

	// Family 0 with Channels > 2 must be rejected
	_, err = BuildOpusHeadPacket(OpusHead{Channels: 6, ChannelMappingFamily: 0})
	if err == nil {
		t.Fatal("expected error for Family 0 with 6 channels, got nil")
	}

	// Truncated OpusHead packet (< 19 bytes)
	_, err = parseOpusHead([]byte("OpusHead\x01\x02\x00"))
	if !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead on short packet, got %v", err)
	}

	// Bad magic signature
	badMagic := make([]byte, 19)
	copy(badMagic, []byte("WrongMag"))
	_, err = parseOpusHead(badMagic)
	if !errors.Is(err, ErrNotOpusOgg) {
		t.Fatalf("expected ErrNotOpusOgg on bad magic, got %v", err)
	}

	// Family 1 with invalid stream counts
	badFamily1 := []byte{
		'O', 'p', 'u', 's', 'H', 'e', 'a', 'd',
		1, 2, // version 1, 2 channels
		0, 0, // preskip
		0x80, 0xbb, 0, 0, // 48000
		0, 0, // gain
		1,    // family 1
		0, 0, // streamCount=0 (invalid!)
		0, 1, // mapping
	}
	_, err = parseOpusHead(badFamily1)
	if err == nil {
		t.Fatal("expected error for StreamCount=0 in Family 1, got nil")
	}
}

func TestRFC7845_OpusTagsRoundtripAndQuery(t *testing.T) {
	tags := OpusTags{
		Vendor: "opus-go test vendor",
		Comments: []string{
			"TITLE=My Song",
			"ARTIST=Go Developer",
			"ALBUM=Best of Opus",
			"TRACKNUMBER=1",
			"GENRE=Electronic",
			"ARTIST=Featured Guest", // multiple ARTIST tags
		},
	}

	pkt, err := BuildOpusTagsPacket(tags)
	if err != nil {
		t.Fatalf("BuildOpusTagsPacket: %v", err)
	}

	parsed, err := parseOpusTags(pkt)
	if err != nil {
		t.Fatalf("parseOpusTags: %v", err)
	}

	if parsed.Vendor != tags.Vendor {
		t.Fatalf("vendor mismatch: got %q, want %q", parsed.Vendor, tags.Vendor)
	}
	if len(parsed.Comments) != len(tags.Comments) {
		t.Fatalf("comment count mismatch: got %d, want %d", len(parsed.Comments), len(tags.Comments))
	}

	// Test Get (case-insensitive)
	if got := parsed.Get("title"); got != "My Song" {
		t.Fatalf("Get('title'): got %q, want 'My Song'", got)
	}
	if got := parsed.Get("TITLE"); got != "My Song" {
		t.Fatalf("Get('TITLE'): got %q, want 'My Song'", got)
	}
	if got := parsed.Get("nonexistent"); got != "" {
		t.Fatalf("Get('nonexistent'): got %q, want empty", got)
	}

	// Test GetAll
	artists := parsed.GetAll("artist")
	if len(artists) != 2 || artists[0] != "Go Developer" || artists[1] != "Featured Guest" {
		t.Fatalf("GetAll('artist') mismatch: %v", artists)
	}
}

func TestRFC7845_OpusReaderHeaderSequence(t *testing.T) {
	// Build a valid Ogg bitstream with OpusHead and OpusTags
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x1234)

	head := OpusHead{Version: 1, Channels: 2, PreSkip: 312, InputSampleRate: 48000}
	headPkt, _ := BuildOpusHeadPacket(head)
	_ = pw.WritePacket(headPkt, 0, true, false)

	tags := OpusTags{Vendor: "opusgo", Comments: []string{"ENCODER=test"}}
	tagsPkt, _ := BuildOpusTagsPacket(tags)
	_ = pw.WritePacket(tagsPkt, 0, false, false)

	// One audio packet
	audioPkt := []byte{0xfc, 0x00, 0x01}
	_ = pw.WritePacket(audioPkt, 960+312, false, true)
	_ = pw.Flush()

	// Read through OpusReader
	r, err := NewOpusReader(&buf)
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}

	if r.Head.Channels != 2 {
		t.Fatalf("expected 2 channels, got %d", r.Head.Channels)
	}
	if r.Tags.Get("ENCODER") != "test" {
		t.Fatalf("expected ENCODER=test, got %q", r.Tags.Get("ENCODER"))
	}

	// Read audio packet
	pkt, err := r.ReadAudioPacket()
	if err != nil {
		t.Fatalf("ReadAudioPacket: %v", err)
	}
	if !bytes.Equal(pkt.Data, audioPkt) {
		t.Fatal("audio packet data mismatch")
	}
	if !pkt.EOS {
		t.Fatal("expected EOS on last audio packet")
	}
}

func TestRFC7845_OpusTags_OOM_DoS_Protection(t *testing.T) {
	// A crafted OpusTags packet claiming 1,000,000 comments in a 20-byte packet
	var buf bytes.Buffer
	buf.Write([]byte("OpusTags"))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.WriteString("test")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(1_000_000)) // Claim 1 million comments!

	_, err := parseOpusTags(buf.Bytes())
	if !errors.Is(err, ErrBadOpusTags) {
		t.Fatalf("expected ErrBadOpusTags on crafted huge comment count, got: %v", err)
	}
}

func TestRFC7845_BuildOpusHeadValidation(t *testing.T) {
	// Family 1 with stream count 0
	_, err := BuildOpusHeadPacket(OpusHead{
		Channels:             6,
		ChannelMappingFamily: 1,
		StreamCount:          0,
		CoupledStreamCount:   0,
		ChannelMapping:       []byte{0, 1, 2, 3, 4, 5},
	})
	if !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead for StreamCount=0, got %v", err)
	}

	// Family 1 with CoupledStreamCount > StreamCount
	_, err = BuildOpusHeadPacket(OpusHead{
		Channels:             6,
		ChannelMappingFamily: 1,
		StreamCount:          2,
		CoupledStreamCount:   3,
		ChannelMapping:       []byte{0, 1, 2, 3, 4, 5},
	})
	if !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead for CoupledStreamCount > StreamCount, got %v", err)
	}

	// Unsupported channel mapping family (e.g. 2, 255)
	_, err = BuildOpusHeadPacket(OpusHead{
		Channels:             2,
		ChannelMappingFamily: 2,
	})
	if !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead for unsupported family 2, got %v", err)
	}

	// Unsupported major version > 0
	_, err = BuildOpusHeadPacket(OpusHead{
		Version:              0x20, // Major version 2
		Channels:             2,
		ChannelMappingFamily: 0,
	})
	if !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead for major version 2, got %v", err)
	}
}

func TestRFC7845_ParseOpusHeadVersionAndFamily(t *testing.T) {
	validHead := OpusHead{
		Version:              1,
		Channels:             2,
		PreSkip:              312,
		InputSampleRate:      48000,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	pkt, err := BuildOpusHeadPacket(validHead)
	if err != nil {
		t.Fatalf("BuildOpusHeadPacket: %v", err)
	}

	// Corrupt version to 0
	badVerPkt := make([]byte, len(pkt))
	copy(badVerPkt, pkt)
	badVerPkt[8] = 0
	if _, err := parseOpusHead(badVerPkt); !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead for version 0, got %v", err)
	}

	// Corrupt version to major version 2 (0x20)
	badVerPkt[8] = 0x20
	if _, err := parseOpusHead(badVerPkt); !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead for major version 2, got %v", err)
	}

	// Corrupt family to 2
	badFamPkt := make([]byte, len(pkt))
	copy(badFamPkt, pkt)
	badFamPkt[18] = 2
	if _, err := parseOpusHead(badFamPkt); !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead for family 2, got %v", err)
	}

	// Corrupt family to 255
	badFamPkt[18] = 255
	if _, err := parseOpusHead(badFamPkt); !errors.Is(err, ErrBadOpusHead) {
		t.Fatalf("expected ErrBadOpusHead for family 255, got %v", err)
	}
}

func TestOpusReader_MaxOpusPacketSize(t *testing.T) {
	var buf bytes.Buffer
	const serial uint32 = 0x11223344
	pw := NewPacketWriter(&buf, serial)

	// Packet 1: OpusHead
	head := OpusHead{
		Version:              1,
		Channels:             2,
		PreSkip:              312,
		InputSampleRate:      48000,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	headPkt, err := BuildOpusHeadPacket(head)
	if err != nil {
		t.Fatalf("BuildOpusHeadPacket: %v", err)
	}
	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		t.Fatalf("WritePacket head: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush head: %v", err)
	}

	// Packet 2: OpusTags
	tagsPkt, err := BuildOpusTagsPacket(OpusTags{Vendor: "test"})
	if err != nil {
		t.Fatalf("BuildOpusTagsPacket: %v", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		t.Fatalf("WritePacket tags: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush tags: %v", err)
	}

	// Packet 3: Huge audio packet (70,000 bytes > 64 KiB MaxOpusPacketSize)
	hugeAudio := make([]byte, 70000)
	hugeAudio[0] = 0xFC // valid TOC (20ms stereo)
	if err := pw.WritePacket(hugeAudio, 960, false, true); err != nil {
		t.Fatalf("WritePacket hugeAudio: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush hugeAudio: %v", err)
	}

	reader, err := NewOpusReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}

	_, err = reader.ReadAudioPacket()
	if !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("expected ErrPacketTooLarge for 70KB audio packet, got %v", err)
	}
}
