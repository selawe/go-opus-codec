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

// MaxFrameSize is the maximum size (1275 bytes) of a single Opus audio frame (RFC 6716 Section 3.1 & 3.2.5).
const MaxFrameSize = 1275

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

// PacketFrames extracts and returns the individual Opus frames from a packet
// according to RFC 6716 Section 3.2 and RFC 8251 Section 3.2.5.
//
// The returned slices reference the underlying packet byte array.
// Returns an error if the packet is malformed, truncated, or exceeds RFC limits (e.g. >120 ms).
func PacketFrames(packet []byte) ([][]byte, error) {
	if len(packet) < 1 {
		return nil, ErrPacketTooShort
	}

	code := int(packet[0] & 0x03)
	switch code {
	case 0:
		// Code 0: exactly 1 frame (RFC 6716 Section 3.2.2)
		if len(packet[1:]) > MaxFrameSize {
			return nil, fmt.Errorf("%w: code 0 frame length %d exceeds max frame size %d", ErrPacketInvalid, len(packet)-1, MaxFrameSize)
		}
		return [][]byte{packet[1:]}, nil

	case 1:
		// Code 1: two CBR frames of equal size (RFC 6716 Section 3.2.3)
		payload := packet[1:]
		if len(payload)%2 != 0 {
			return nil, fmt.Errorf("%w: code 1 packet has odd payload length %d", ErrPacketInvalid, len(payload))
		}
		frameSize := len(payload) / 2
		if frameSize > MaxFrameSize {
			return nil, fmt.Errorf("%w: code 1 frame length %d exceeds max frame size %d", ErrPacketInvalid, frameSize, MaxFrameSize)
		}
		return [][]byte{payload[:frameSize], payload[frameSize:]}, nil

	case 2:
		// Code 2: two VBR frames (RFC 6716 Section 3.2.4)
		payload := packet[1:]
		if len(payload) < 1 {
			return nil, fmt.Errorf("%w: missing frame 0 size in code 2 packet", ErrPacketTooShort)
		}
		frame0Size, headerBytes, err := parseFrameSize(payload)
		if err != nil {
			return nil, err
		}
		if len(payload) < headerBytes+frame0Size {
			return nil, fmt.Errorf("%w: code 2 frame 0 exceeds packet length", ErrPacketInvalid)
		}
		frame0 := payload[headerBytes : headerBytes+frame0Size]
		frame1 := payload[headerBytes+frame0Size:]
		if frame0Size > MaxFrameSize || len(frame1) > MaxFrameSize {
			return nil, fmt.Errorf("%w: code 2 frame exceeds max frame size %d", ErrPacketInvalid, MaxFrameSize)
		}
		return [][]byte{frame0, frame1}, nil

	case 3:
		// Code 3: arbitrary number of frames (RFC 6716 Section 3.2.5 & RFC 8251)
		if len(packet) < 2 {
			return nil, fmt.Errorf("%w: missing frame count byte", ErrPacketTooShort)
		}
		frameByte := packet[1]
		count := int(frameByte & 0x3F)
		if count < 1 || count > 48 {
			return nil, fmt.Errorf("%w: invalid frame count %d (must be 1..48)", ErrPacketInvalid, count)
		}

		// Verify 120ms limit per RFC 6716 Section 3.2.5
		samplesPerFrame := PacketSamplesPerFrame(packet, 48000)
		if samplesPerFrame*count > 5760 {
			return nil, fmt.Errorf("%w: packet duration %d samples exceeds 120ms (5760 samples)", ErrPacketExcessFrames, samplesPerFrame*count)
		}

		hasPadding := (frameByte & 0x40) != 0
		isVBR := (frameByte & 0x80) != 0

		offset := 2
		totalPadding := 0
		if hasPadding {
			for {
				if offset >= len(packet) {
					return nil, fmt.Errorf("%w: truncated padding length sequence", ErrPacketTooShort)
				}
				p := int(packet[offset])
				offset++
				if p == 255 {
					totalPadding += 254
				} else {
					totalPadding += p
					break
				}
				if offset+totalPadding > len(packet) {
					return nil, fmt.Errorf("%w: padding %d exceeds packet remaining payload %d", ErrPacketInvalid, totalPadding, len(packet)-offset)
				}
			}
		}

		if offset+totalPadding > len(packet) {
			return nil, fmt.Errorf("%w: padding %d exceeds packet remaining payload %d", ErrPacketInvalid, totalPadding, len(packet)-offset)
		}

		payloadLen := len(packet) - offset - totalPadding
		payload := packet[offset : offset+payloadLen]

		if !isVBR {
			// CBR frames
			if payloadLen%count != 0 {
				return nil, fmt.Errorf("%w: CBR payload %d not evenly divisible by frame count %d", ErrPacketInvalid, payloadLen, count)
			}
			frameSize := payloadLen / count
			if frameSize > MaxFrameSize {
				return nil, fmt.Errorf("%w: code 3 CBR frame length %d exceeds max frame size %d", ErrPacketInvalid, frameSize, MaxFrameSize)
			}
			frames := make([][]byte, count)
			for i := 0; i < count; i++ {
				frames[i] = payload[i*frameSize : (i+1)*frameSize]
			}
			return frames, nil
		}

		// VBR frames
		frames := make([][]byte, count)
		curr := payload
		for i := 0; i < count-1; i++ {
			frameSize, hdrBytes, err := parseFrameSize(curr)
			if err != nil {
				return nil, err
			}
			curr = curr[hdrBytes:]
			if len(curr) < frameSize {
				return nil, fmt.Errorf("%w: VBR frame %d length %d exceeds remaining payload %d", ErrPacketInvalid, i, frameSize, len(curr))
			}
			frames[i] = curr[:frameSize]
			curr = curr[frameSize:]
		}
		// Last frame receives remaining payload bytes
		if len(curr) > MaxFrameSize {
			return nil, fmt.Errorf("%w: code 3 VBR last frame length %d exceeds max frame size %d", ErrPacketInvalid, len(curr), MaxFrameSize)
		}
		frames[count-1] = curr
		return frames, nil

	default:
		return nil, ErrPacketInvalid
	}
}

func parseFrameSize(data []byte) (int, int, error) {
	if len(data) < 1 {
		return 0, 0, ErrPacketTooShort
	}
	b0 := int(data[0])
	if b0 < 252 {
		return b0, 1, nil
	}
	if len(data) < 2 {
		return 0, 0, ErrPacketTooShort
	}
	b1 := int(data[1])
	return 4*b1 + b0, 2, nil
}

// PacketHasLBRR reports whether the Opus packet contains Low Bit-Rate Redundancy
// (in-band Forward Error Correction, FEC) for SILK/Hybrid modes (RFC 6716 Section 4.5.2).
func PacketHasLBRR(packet []byte) (bool, error) {
	if len(packet) < 1 {
		return false, ErrPacketTooShort
	}

	mode, _, _, _, _ := ParsePacketTOC(packet[0])
	if mode == ModeCelt {
		// CELT frames do not contain SILK LBRR data
		return false, nil
	}

	frames, err := PacketFrames(packet)
	if err != nil {
		return false, err
	}
	if len(frames) == 0 || len(frames[0]) == 0 {
		return false, nil
	}

	// Determine number of 20ms SILK sub-frames per frame (1..3)
	samplesPerFrame := PacketSamplesPerFrame(packet, 48000)
	nbFrames := 1
	if samplesPerFrame > 960 {
		nbFrames = samplesPerFrame / 960
	}
	if nbFrames > 3 {
		nbFrames = 3
	}

	channels := PacketChannels(packet)
	firstByte := int(frames[0][0])

	// RFC 6716 Section 4.2.2: LBRR flag for primary channel is at bit (7 - nbFrames)
	lbrr := (firstByte >> (7 - nbFrames) & 0x01) != 0
	if channels == 2 {
		// Stereo channel LBRR flag is at bit (6 - 2*nbFrames)
		lbrr = lbrr || ((firstByte >> (6 - 2*nbFrames) & 0x01) != 0)
	}

	return lbrr, nil
}
