package opus

import (
	"errors"
	"fmt"
	"time"
)

// Mode represents the Opus encoding mode (RFC 6716 Section 3.1).
type Mode int

const (
	ModeSilk Mode = iota
	ModeHybrid
	ModeCelt
)

func (m Mode) String() string {
	switch m {
	case ModeSilk:
		return "SILK"
	case ModeHybrid:
		return "Hybrid"
	case ModeCelt:
		return "CELT"
	default:
		return "Unknown"
	}
}

// Bandwidth represents the Opus audio bandwidth (RFC 6716 Section 3.1).
type Bandwidth int

const (
	BandwidthNarrowband    Bandwidth = 1101 // 8 kHz
	BandwidthMediumband    Bandwidth = 1102 // 12 kHz
	BandwidthWideband      Bandwidth = 1103 // 16 kHz
	BandwidthSuperwideband Bandwidth = 1104 // 24 kHz
	BandwidthFullband      Bandwidth = 1105 // 20 kHz audio / 48 kHz internal
)

func (b Bandwidth) String() string {
	switch b {
	case BandwidthNarrowband:
		return "Narrowband (8 kHz)"
	case BandwidthMediumband:
		return "Mediumband (12 kHz)"
	case BandwidthWideband:
		return "Wideband (16 kHz)"
	case BandwidthSuperwideband:
		return "Superwideband (24 kHz)"
	case BandwidthFullband:
		return "Fullband (48 kHz)"
	default:
		return "Unknown"
	}
}

var (
	ErrPacketTooShort     = errors.New("opus: packet too short")
	ErrPacketInvalid      = errors.New("opus: invalid packet")
	ErrPacketExcessFrames = errors.New("opus: packet exceeds 120ms limit")
)

// ParsePacketTOC parses the Opus Table of Contents (TOC) byte (RFC 6716 Section 3.1).
// It extracts mode, audio bandwidth, frame duration, stereo flag, and frame count code (0..3).
func ParsePacketTOC(toc byte) (mode Mode, bw Bandwidth, frameDuration time.Duration, stereo bool, code int) {
	config := int(toc >> 3)
	stereo = (toc & 0x04) != 0
	code = int(toc & 0x03)

	if config < 12 {
		mode = ModeSilk
		switch config / 4 {
		case 0:
			bw = BandwidthNarrowband
		case 1:
			bw = BandwidthMediumband
		case 2:
			bw = BandwidthWideband
		}
		switch config % 4 {
		case 0:
			frameDuration = 10 * time.Millisecond
		case 1:
			frameDuration = 20 * time.Millisecond
		case 2:
			frameDuration = 40 * time.Millisecond
		case 3:
			frameDuration = 60 * time.Millisecond
		}
	} else if config < 16 {
		mode = ModeHybrid
		if config < 14 {
			bw = BandwidthSuperwideband
		} else {
			bw = BandwidthFullband
		}
		if (config % 2) == 0 {
			frameDuration = 10 * time.Millisecond
		} else {
			frameDuration = 20 * time.Millisecond
		}
	} else {
		mode = ModeCelt
		switch (config - 16) / 4 {
		case 0:
			bw = BandwidthNarrowband
		case 1:
			bw = BandwidthWideband
		case 2:
			bw = BandwidthSuperwideband
		case 3:
			bw = BandwidthFullband
		}
		switch config % 4 {
		case 0:
			frameDuration = 2500 * time.Microsecond // 2.5 ms
		case 1:
			frameDuration = 5 * time.Millisecond
		case 2:
			frameDuration = 10 * time.Millisecond
		case 3:
			frameDuration = 20 * time.Millisecond
		}
	}
	return
}

// PacketChannels returns the number of channels (1 or 2) signaled in the packet TOC byte.
func PacketChannels(packet []byte) int {
	if len(packet) < 1 {
		return 0
	}
	if (packet[0] & 0x04) != 0 {
		return 2
	}
	return 1
}

// PacketBandwidth returns the audio bandwidth of the packet (RFC 6716 Section 3.1).
func PacketBandwidth(packet []byte) Bandwidth {
	if len(packet) < 1 {
		return 0
	}
	_, bw, _, _, _ := ParsePacketTOC(packet[0])
	return bw
}

// PacketFrameCount returns the number of frames contained in the packet (RFC 6716 Section 3.2).
// Returns 1 to 48, or an error if the packet is malformed.
func PacketFrameCount(packet []byte) (int, error) {
	if len(packet) < 1 {
		return 0, ErrPacketTooShort
	}
	code := int(packet[0] & 0x03)
	switch code {
	case 0:
		return 1, nil
	case 1, 2:
		return 2, nil
	case 3:
		if len(packet) < 2 {
			return 0, fmt.Errorf("%w: missing frame count byte", ErrPacketTooShort)
		}
		count := int(packet[1] & 0x3F)
		if count < 1 || count > 48 {
			return 0, fmt.Errorf("%w: invalid frame count %d (must be 1..48)", ErrPacketInvalid, count)
		}
		return count, nil
	default:
		return 0, ErrPacketInvalid
	}
}

// PacketSamplesPerFrame returns the number of samples per channel in each frame at the given sample rate.
func PacketSamplesPerFrame(packet []byte, sampleRate int) int {
	if len(packet) < 1 {
		return 0
	}
	_, _, duration, _, _ := ParsePacketTOC(packet[0])
	return int(int64(duration) * int64(sampleRate) / int64(time.Second))
}

// PacketTotalSamples returns the total number of samples per channel in the packet at the given sample rate.
func PacketTotalSamples(packet []byte, sampleRate int) (int, error) {
	count, err := PacketFrameCount(packet)
	if err != nil {
		return 0, err
	}
	samplesPerFrame := PacketSamplesPerFrame(packet, sampleRate)
	total := count * samplesPerFrame
	// RFC 6716 Section 3.2.5: Total duration cannot exceed 120 ms.
	maxSamples := sampleRate * 120 / 1000
	if total > maxSamples {
		return 0, fmt.Errorf("%w: %d samples exceeds %d for 120ms", ErrPacketExcessFrames, total, maxSamples)
	}
	return total, nil
}

// PacketDuration returns the total playback duration of the packet (RFC 6716).
func PacketDuration(packet []byte) (time.Duration, error) {
	count, err := PacketFrameCount(packet)
	if err != nil {
		return 0, err
	}
	_, _, frameDur, _, _ := ParsePacketTOC(packet[0])
	total := time.Duration(count) * frameDur
	if total > 120*time.Millisecond {
		return 0, fmt.Errorf("%w: %v exceeds 120ms", ErrPacketExcessFrames, total)
	}
	return total, nil
}
