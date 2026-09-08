package opus

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// Table 1 of RFC 6716:
// Config 0..11: SILK-only
// Config 12..15: Hybrid
// Config 16..31: CELT-only
func TestRFC6716_TOCConfigurations(t *testing.T) {
	expectedConfigs := []struct {
		config   int
		mode     Mode
		bw       Bandwidth
		duration time.Duration
	}{
		// SILK Narrowband (configs 0..3)
		{0, ModeSilk, BandwidthNarrowband, 10 * time.Millisecond},
		{1, ModeSilk, BandwidthNarrowband, 20 * time.Millisecond},
		{2, ModeSilk, BandwidthNarrowband, 40 * time.Millisecond},
		{3, ModeSilk, BandwidthNarrowband, 60 * time.Millisecond},

		// SILK Mediumband (configs 4..7)
		{4, ModeSilk, BandwidthMediumband, 10 * time.Millisecond},
		{5, ModeSilk, BandwidthMediumband, 20 * time.Millisecond},
		{6, ModeSilk, BandwidthMediumband, 40 * time.Millisecond},
		{7, ModeSilk, BandwidthMediumband, 60 * time.Millisecond},

		// SILK Wideband (configs 8..11)
		{8, ModeSilk, BandwidthWideband, 10 * time.Millisecond},
		{9, ModeSilk, BandwidthWideband, 20 * time.Millisecond},
		{10, ModeSilk, BandwidthWideband, 40 * time.Millisecond},
		{11, ModeSilk, BandwidthWideband, 60 * time.Millisecond},

		// Hybrid Superwideband (configs 12..13)
		{12, ModeHybrid, BandwidthSuperwideband, 10 * time.Millisecond},
		{13, ModeHybrid, BandwidthSuperwideband, 20 * time.Millisecond},

		// Hybrid Fullband (configs 14..15)
		{14, ModeHybrid, BandwidthFullband, 10 * time.Millisecond},
		{15, ModeHybrid, BandwidthFullband, 20 * time.Millisecond},

		// CELT Narrowband (configs 16..19)
		{16, ModeCelt, BandwidthNarrowband, 2500 * time.Microsecond},
		{17, ModeCelt, BandwidthNarrowband, 5 * time.Millisecond},
		{18, ModeCelt, BandwidthNarrowband, 10 * time.Millisecond},
		{19, ModeCelt, BandwidthNarrowband, 20 * time.Millisecond},

		// CELT Wideband (configs 20..23)
		{20, ModeCelt, BandwidthWideband, 2500 * time.Microsecond},
		{21, ModeCelt, BandwidthWideband, 5 * time.Millisecond},
		{22, ModeCelt, BandwidthWideband, 10 * time.Millisecond},
		{23, ModeCelt, BandwidthWideband, 20 * time.Millisecond},

		// CELT Superwideband (configs 24..27)
		{24, ModeCelt, BandwidthSuperwideband, 2500 * time.Microsecond},
		{25, ModeCelt, BandwidthSuperwideband, 5 * time.Millisecond},
		{26, ModeCelt, BandwidthSuperwideband, 10 * time.Millisecond},
		{27, ModeCelt, BandwidthSuperwideband, 20 * time.Millisecond},

		// CELT Fullband (configs 28..31)
		{28, ModeCelt, BandwidthFullband, 2500 * time.Microsecond},
		{29, ModeCelt, BandwidthFullband, 5 * time.Millisecond},
		{30, ModeCelt, BandwidthFullband, 10 * time.Millisecond},
		{31, ModeCelt, BandwidthFullband, 20 * time.Millisecond},
	}

	for _, exp := range expectedConfigs {
		// Construct TOC byte: config in bits 7..3, stereo in bit 2, code in bits 1..0
		tocMono := byte(exp.config << 3)
		tocStereo := byte(exp.config<<3) | 0x04

		// Test mono
		mode, bw, dur, stereo, code := ParsePacketTOC(tocMono)
		if mode != exp.mode {
			t.Fatalf("config %d mono mode mismatch: got %v, want %v", exp.config, mode, exp.mode)
		}
		if bw != exp.bw {
			t.Fatalf("config %d mono bw mismatch: got %v, want %v", exp.config, bw, exp.bw)
		}
		if dur != exp.duration {
			t.Fatalf("config %d mono duration mismatch: got %v, want %v", exp.config, dur, exp.duration)
		}
		if stereo {
			t.Fatalf("config %d expected mono, got stereo", exp.config)
		}
		if code != 0 {
			t.Fatalf("config %d expected code 0, got %d", exp.config, code)
		}

		// Test stereo
		mode, bw, dur, stereo, _ = ParsePacketTOC(tocStereo)
		if !stereo {
			t.Fatalf("config %d expected stereo, got mono", exp.config)
		}
		if mode != exp.mode || bw != exp.bw || dur != exp.duration {
			t.Fatalf("config %d stereo properties mismatch", exp.config)
		}
	}
}

func TestRFC6716_FrameCountCodes(t *testing.T) {
	// Config 16 (CELT 2.5ms)
	const cfg = 16 << 3

	// Code 0: 1 frame
	pktCode0 := []byte{cfg | 0, 0xAA, 0xBB}
	count, err := PacketFrameCount(pktCode0)
	if err != nil || count != 1 {
		t.Fatalf("Code 0: want count=1, got count=%d err=%v", count, err)
	}

	// Code 1: 2 equal frames
	pktCode1 := []byte{cfg | 1, 0xAA, 0xBB}
	count, err = PacketFrameCount(pktCode1)
	if err != nil || count != 2 {
		t.Fatalf("Code 1: want count=2, got count=%d err=%v", count, err)
	}

	// Code 2: 2 different frames
	pktCode2 := []byte{cfg | 2, 0x05, 0xAA, 0xBB}
	count, err = PacketFrameCount(pktCode2)
	if err != nil || count != 2 {
		t.Fatalf("Code 2: want count=2, got count=%d err=%v", count, err)
	}

	// Code 3: Arbitrary frames (M=3 frames, CBR, no padding)
	// byte 1: 0b00000011 = 3 frames
	pktCode3 := []byte{cfg | 3, 3, 0x11, 0x22, 0x33}
	count, err = PacketFrameCount(pktCode3)
	if err != nil || count != 3 {
		t.Fatalf("Code 3 (M=3): want count=3, got count=%d err=%v", count, err)
	}

	// Code 3: VBR with padding bit set (0b11000100 = VBR | P | 4 frames)
	pktCode3Padded := []byte{cfg | 3, 0xC4, 0x00}
	count, err = PacketFrameCount(pktCode3Padded)
	if err != nil || count != 4 {
		t.Fatalf("Code 3 (M=4 padded): want count=4, got count=%d err=%v", count, err)
	}
}

func TestRFC6716_PacketLimitsAndErrors(t *testing.T) {
	// Empty packet
	if _, err := PacketFrameCount(nil); !errors.Is(err, ErrPacketTooShort) {
		t.Fatalf("expected ErrPacketTooShort on nil packet, got: %v", err)
	}
	if _, err := PacketDuration([]byte{}); !errors.Is(err, ErrPacketTooShort) {
		t.Fatalf("expected ErrPacketTooShort on empty packet, got: %v", err)
	}

	// Code 3 with only TOC byte (missing frame count byte)
	if _, err := PacketFrameCount([]byte{0x03}); !errors.Is(err, ErrPacketTooShort) {
		t.Fatalf("expected ErrPacketTooShort for truncated Code 3, got: %v", err)
	}

	// Code 3 with M=0 (invalid per RFC 6716 Section 3.2.5)
	if _, err := PacketFrameCount([]byte{0x03, 0x00}); !errors.Is(err, ErrPacketInvalid) {
		t.Fatalf("expected ErrPacketInvalid for M=0, got: %v", err)
	}

	// Code 3 with M=49 (> 48 is invalid)
	if _, err := PacketFrameCount([]byte{0x03, 49}); !errors.Is(err, ErrPacketInvalid) {
		t.Fatalf("expected ErrPacketInvalid for M=49, got: %v", err)
	}

	// Total duration exceeding 120ms (RFC 6716 Section 3.2.5)
	// Config 1: 20ms frame duration. With M=7 frames -> 140ms > 120ms
	toc20ms := byte(1 << 3)
	pktExcess := []byte{toc20ms | 3, 7}
	if _, err := PacketDuration(pktExcess); !errors.Is(err, ErrPacketExcessFrames) {
		t.Fatalf("expected ErrPacketExcessFrames for 140ms, got: %v", err)
	}
	if _, err := PacketTotalSamples(pktExcess, 48000); !errors.Is(err, ErrPacketExcessFrames) {
		t.Fatalf("expected ErrPacketExcessFrames for total samples, got: %v", err)
	}
}

func TestRFC6716_PacketInspectionHelpers(t *testing.T) {
	// Fullband stereo CELT 20ms packet (Config 31, stereo, 1 frame)
	// Config 31 << 3 = 248 | 0x04 (stereo) | 0 (Code 0) = 252 (0xFC)
	pkt := []byte{0xFC, 0x01, 0x02, 0x03}

	if channels := PacketChannels(pkt); channels != 2 {
		t.Fatalf("PacketChannels: got %d, want 2", channels)
	}
	if bw := PacketBandwidth(pkt); bw != BandwidthFullband {
		t.Fatalf("PacketBandwidth: got %v, want Fullband", bw)
	}
	if count, err := PacketFrameCount(pkt); err != nil || count != 1 {
		t.Fatalf("PacketFrameCount: got %d, err %v", count, err)
	}

	// 20ms at 48kHz is 960 samples/channel
	if spf := PacketSamplesPerFrame(pkt, 48000); spf != 960 {
		t.Fatalf("PacketSamplesPerFrame: got %d, want 960", spf)
	}
	if total, err := PacketTotalSamples(pkt, 48000); err != nil || total != 960 {
		t.Fatalf("PacketTotalSamples: got %d, want 960", total)
	}
	if dur, err := PacketDuration(pkt); err != nil || dur != 20*time.Millisecond {
		t.Fatalf("PacketDuration: got %v, want 20ms", dur)
	}
}

func TestPacketFrames_Code0(t *testing.T) {
	pkt := []byte{0x00, 0x11, 0x22, 0x33}
	frames, err := PacketFrames(pkt)
	if err != nil {
		t.Fatalf("PacketFrames Code 0: %v", err)
	}
	if len(frames) != 1 || !bytes.Equal(frames[0], []byte{0x11, 0x22, 0x33}) {
		t.Fatalf("unexpected frames: %v", frames)
	}
}

func TestPacketFrames_Code1_CBR(t *testing.T) {
	// Even payload: 4 bytes -> 2 frames of 2 bytes
	pkt := []byte{0x01, 0x11, 0x22, 0x33, 0x44}
	frames, err := PacketFrames(pkt)
	if err != nil {
		t.Fatalf("PacketFrames Code 1: %v", err)
	}
	if len(frames) != 2 || !bytes.Equal(frames[0], []byte{0x11, 0x22}) || !bytes.Equal(frames[1], []byte{0x33, 0x44}) {
		t.Fatalf("unexpected frames: %v", frames)
	}

	// Odd payload: should return ErrPacketInvalid
	pktOdd := []byte{0x01, 0x11, 0x22, 0x33}
	if _, err := PacketFrames(pktOdd); !errors.Is(err, ErrPacketInvalid) {
		t.Fatalf("expected ErrPacketInvalid for odd Code 1 payload, got: %v", err)
	}
}

func TestPacketFrames_Code2_VBR(t *testing.T) {
	// Frame 0 size = 3 (1-byte header: 0x03), Frame 1 size = 2
	pkt := []byte{0x02, 0x03, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	frames, err := PacketFrames(pkt)
	if err != nil {
		t.Fatalf("PacketFrames Code 2: %v", err)
	}
	if len(frames) != 2 || !bytes.Equal(frames[0], []byte{0xAA, 0xBB, 0xCC}) || !bytes.Equal(frames[1], []byte{0xDD, 0xEE}) {
		t.Fatalf("unexpected frames: %v", frames)
	}

	// 2-byte size header: 252 + 4*1 = 256 bytes for frame 0
	frame0 := bytes.Repeat([]byte{0xA5}, 256)
	frame1 := []byte{0x01, 0x02, 0x03}
	pkt2 := append([]byte{0x02, 252, 1}, frame0...)
	pkt2 = append(pkt2, frame1...)
	frames2, err := PacketFrames(pkt2)
	if err != nil {
		t.Fatalf("PacketFrames Code 2 (2-byte header): %v", err)
	}
	if len(frames2) != 2 || !bytes.Equal(frames2[0], frame0) || !bytes.Equal(frames2[1], frame1) {
		t.Fatalf("unexpected frames2: frame0 len=%d, frame1 len=%d", len(frames2[0]), len(frames2[1]))
	}

	// Truncated size header
	if _, err := PacketFrames([]byte{0x02}); !errors.Is(err, ErrPacketTooShort) {
		t.Fatalf("expected ErrPacketTooShort for truncated Code 2, got: %v", err)
	}
}

func TestPacketFrames_Code3_CBR(t *testing.T) {
	// 3 frames, CBR, no padding: count=3, payload len=6 -> 2 bytes per frame
	pkt := []byte{0x03, 3, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	frames, err := PacketFrames(pkt)
	if err != nil {
		t.Fatalf("PacketFrames Code 3 CBR: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	if !bytes.Equal(frames[0], []byte{0x01, 0x02}) ||
		!bytes.Equal(frames[1], []byte{0x03, 0x04}) ||
		!bytes.Equal(frames[2], []byte{0x05, 0x06}) {
		t.Fatalf("unexpected frames content: %v", frames)
	}

	// Undivisible payload
	pktBad := []byte{0x03, 3, 0x01, 0x02, 0x03, 0x04}
	if _, err := PacketFrames(pktBad); !errors.Is(err, ErrPacketInvalid) {
		t.Fatalf("expected ErrPacketInvalid for indivisible CBR, got: %v", err)
	}
}

func TestPacketFrames_Code3_VBR(t *testing.T) {
	// 3 frames, VBR (bit 7 set: 0x83), frame 0 size=2, frame 1 size=3, frame 2=remainder (4 bytes)
	pkt := []byte{0x03, 0x83, 2, 0x10, 0x11, 3, 0x20, 0x21, 0x22, 0x30, 0x31, 0x32, 0x33}
	frames, err := PacketFrames(pkt)
	if err != nil {
		t.Fatalf("PacketFrames Code 3 VBR: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	if !bytes.Equal(frames[0], []byte{0x10, 0x11}) ||
		!bytes.Equal(frames[1], []byte{0x20, 0x21, 0x22}) ||
		!bytes.Equal(frames[2], []byte{0x30, 0x31, 0x32, 0x33}) {
		t.Fatalf("unexpected VBR frames: %v", frames)
	}
}

func TestPacketFrames_Code3_Padding(t *testing.T) {
	// 2 frames, CBR with padding (bit 6 set: 0x42)
	// Padding bytes: 255, 3 -> 254 + 3 = 257 bytes of padding at end
	padding := make([]byte, 257)
	pkt := []byte{0x03, 0x42, 255, 3, 0xAA, 0xBB, 0xCC, 0xDD}
	pkt = append(pkt, padding...)

	frames, err := PacketFrames(pkt)
	if err != nil {
		t.Fatalf("PacketFrames Code 3 padded: %v", err)
	}
	if len(frames) != 2 || !bytes.Equal(frames[0], []byte{0xAA, 0xBB}) || !bytes.Equal(frames[1], []byte{0xCC, 0xDD}) {
		t.Fatalf("unexpected padded frames: %v", frames)
	}
}

func TestPacketHasLBRR(t *testing.T) {
	// CELT packet (config 20): LBRR is always false
	celtPkt := []byte{20 << 3, 0x11, 0x22}
	has, err := PacketHasLBRR(celtPkt)
	if err != nil || has {
		t.Fatalf("CELT packet: want false, got %v err %v", has, err)
	}

	// SILK 20ms mono packet (config 1, nbFrames=1)
	// LBRR bit for mono 20ms is bit (7-1) = bit 6 (0x40)
	silkWithLBRR := []byte{1 << 3, 0x40, 0x12, 0x34}
	has, err = PacketHasLBRR(silkWithLBRR)
	if err != nil || !has {
		t.Fatalf("SILK with LBRR: want true, got %v err %v", has, err)
	}

	silkWithoutLBRR := []byte{1 << 3, 0x00, 0x12, 0x34}
	has, err = PacketHasLBRR(silkWithoutLBRR)
	if err != nil || has {
		t.Fatalf("SILK without LBRR: want false, got %v err %v", has, err)
	}
}
