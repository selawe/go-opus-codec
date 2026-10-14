// Package rtp implements RFC 7587 (RTP Payload Format for the Opus Speech and Audio Codec)
// and RFC 3550 RTP packet serialization and depacketization.
//
// Key features:
//   - Packetizer: Wraps Opus frames into RFC 7587 compliant RTP packets with 48 kHz timestamping.
//   - Depacketizer: Extracts Opus payloads and detects packet loss gaps for Packet Loss Concealment (PLC).
//   - 100% pure Go with zero external dependencies.
package rtp

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/selawe/go-opus-codec/opus"
)

var (
	ErrPacketTooShort        = errors.New("rtp: packet is too short for header")
	ErrUnsupportedVersion    = errors.New("rtp: unsupported RTP version (must be 2)")
	ErrEmptyOpusPayload      = errors.New("rtp: empty opus payload")
	ErrExtensionNotSupported = errors.New("rtp: extension header not supported in marshal")
	ErrPaddingNotSupported   = errors.New("rtp: padding not supported in marshal")
	ErrTooManyCSRCs          = errors.New("rtp: CSRC count cannot exceed 15")
	ErrInvalidPayloadType    = errors.New("rtp: payload type must be <= 127")

	// ErrInvalidPadding is returned by Unmarshal when the padding bit is set but the padding count is missing,
	// zero (RFC 3550 counts the count byte itself, so it is at least 1) or larger than the packet. It matches
	// ErrPacketTooShort with errors.Is.
	ErrInvalidPadding = fmt.Errorf("rtp: invalid padding: %w", ErrPacketTooShort)

	// ErrDuplicatePacket is returned by Depacketizer.Depacketize for a packet with the sequence number of the one
	// accepted last. It carries no payload: drop it, or the same audio is decoded twice.
	ErrDuplicatePacket = errors.New("rtp: duplicate packet")

	// ErrPayloadTooLarge is returned by Packetizer when an Opus packet exceeds the packetizer's MaxPayloadSize.
	ErrPayloadTooLarge = errors.New("rtp: opus payload larger than the packetizer's MaxPayloadSize")

	// ErrLatePacket is returned by Depacketizer.Depacketize for a packet older than one already accepted. It carries
	// no payload: drop it, since decoding it now would play audio out of order. Put a jitter buffer in front of the
	// Depacketizer if reordered packets should be put back in sequence rather than dropped.
	ErrLatePacket = errors.New("rtp: late packet (older than one already accepted)")
)

// HeaderLength is the minimum RTP header length in bytes per RFC 3550.
const HeaderLength = 12

// ClockRate is the standard RTP timestamp clock rate (48000 Hz) mandated by RFC 7587 §4.1.
const ClockRate = 48000

// Packet represents an RTP packet carrying an Opus payload (RFC 3550 & RFC 7587).
type Packet struct {
	Version        uint8
	Padding        bool
	Extension      bool
	Marker         bool
	PayloadType    uint8
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
	CSRC           []uint32
	Payload        []byte
}

// Marshal serializes the Packet into RFC 3550 binary wire format.
// It returns an error if Extension or Padding is set without support, if len(CSRC) > 15,
// or if PayloadType exceeds 127.
func (p *Packet) Marshal() ([]byte, error) {
	if p == nil {
		return nil, errors.New("rtp: packet is nil")
	}
	if p.Version != 0 && p.Version != 2 {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, p.Version)
	}
	if p.Extension {
		return nil, ErrExtensionNotSupported
	}
	if p.Padding {
		return nil, ErrPaddingNotSupported
	}
	if len(p.CSRC) > 15 {
		return nil, ErrTooManyCSRCs
	}
	if p.PayloadType > 127 {
		return nil, ErrInvalidPayloadType
	}

	csrcLen := len(p.CSRC) * 4
	totalLen := HeaderLength + csrcLen + len(p.Payload)
	buf := make([]byte, totalLen)

	v := uint8(2)
	b0 := (v << 6) & 0xC0
	b0 |= uint8(len(p.CSRC)) & 0x0F
	buf[0] = b0

	b1 := p.PayloadType & 0x7F
	if p.Marker {
		b1 |= 0x80
	}
	buf[1] = b1

	binary.BigEndian.PutUint16(buf[2:4], p.SequenceNumber)
	binary.BigEndian.PutUint32(buf[4:8], p.Timestamp)
	binary.BigEndian.PutUint32(buf[8:12], p.SSRC)

	off := HeaderLength
	for _, csrc := range p.CSRC {
		binary.BigEndian.PutUint32(buf[off:off+4], csrc)
		off += 4
	}

	copy(buf[off:], p.Payload)
	return buf, nil
}

// Unmarshal parses an RTP packet from binary wire format.
func (p *Packet) Unmarshal(data []byte) error {
	if len(data) < HeaderLength {
		return ErrPacketTooShort
	}

	p.Version = data[0] >> 6
	if p.Version != 2 {
		return fmt.Errorf("%w: got %d", ErrUnsupportedVersion, p.Version)
	}
	p.Padding = (data[0] & 0x20) != 0
	p.Extension = (data[0] & 0x10) != 0
	csrcCount := int(data[0] & 0x0F)

	p.Marker = (data[1] & 0x80) != 0
	p.PayloadType = data[1] & 0x7F

	p.SequenceNumber = binary.BigEndian.Uint16(data[2:4])
	p.Timestamp = binary.BigEndian.Uint32(data[4:8])
	p.SSRC = binary.BigEndian.Uint32(data[8:12])

	off := HeaderLength
	csrcBytes := csrcCount * 4
	if len(data) < off+csrcBytes {
		return ErrPacketTooShort
	}

	if csrcCount > 0 {
		p.CSRC = make([]uint32, csrcCount)
		for i := 0; i < csrcCount; i++ {
			p.CSRC[i] = binary.BigEndian.Uint32(data[off : off+4])
			off += 4
		}
	} else {
		p.CSRC = nil
	}

	// Extension header handling if present
	if p.Extension {
		if len(data) < off+4 {
			return ErrPacketTooShort
		}
		extLen := int(binary.BigEndian.Uint16(data[off+2:off+4])) * 4
		off += 4 + extLen
		if len(data) < off {
			return ErrPacketTooShort
		}
	}

	payloadEnd := len(data)
	if p.Padding {
		// The last byte holds the padding count, which includes that byte itself (RFC 3550 section 5.1).
		if payloadEnd <= off {
			return ErrInvalidPadding
		}
		paddingLen := int(data[payloadEnd-1])
		if paddingLen < 1 || paddingLen > payloadEnd-off {
			return ErrInvalidPadding
		}
		payloadEnd -= paddingLen
	}

	p.Payload = data[off:payloadEnd]
	return nil
}

// DefaultMaxPayloadSize is the largest Opus payload a Packetizer accepts unless MaxPayloadSize says otherwise: 1200
// bytes, the conventional safe size for WebRTC, which leaves room for the RTP header (and SRTP and extension
// overhead) inside a 1280-byte path MTU. One Opus frame is at most 1275 bytes, but a normal packet is a few hundred.
const DefaultMaxPayloadSize = 1200

// Packetizer wraps Opus audio frames into RFC 7587 compliant RTP packets. It is not safe for concurrent use.
type Packetizer struct {
	// MaxPayloadSize bounds the Opus packets Packetize accepts (ErrPayloadTooLarge above it). Zero means
	// DefaultMaxPayloadSize; a negative value turns the check off.
	MaxPayloadSize int

	ssrc        uint32
	payloadType uint8
	seq         uint16
	timestamp   uint32
}

// NewPacketizer creates a new RTP Packetizer for Opus audio streams.
func NewPacketizer(ssrc uint32, payloadType uint8, initialSeq uint16, initialTimestamp uint32) *Packetizer {
	return &Packetizer{
		ssrc:        ssrc,
		payloadType: payloadType,
		seq:         initialSeq,
		timestamp:   initialTimestamp,
	}
}

// Packetize wraps an Opus encoded packet into an RTP Packet with the marker bit clear.
// It inspects the Opus packet TOC to compute the exact 48 kHz timestamp delta per RFC 7587 §4.1.
// The returned Packet's Payload is opusPacket itself, not a copy.
func (p *Packetizer) Packetize(opusPacket []byte) (*Packet, error) {
	return p.PacketizeMarker(opusPacket, false)
}

// PacketizeMarker is Packetize with control of the marker bit. RFC 7587 (with RFC 3551) has the sender set it on the
// first packet after a period of silence, that is when it resumes after DTX (discontinuous transmission) or a mute.
func (p *Packetizer) PacketizeMarker(opusPacket []byte, marker bool) (*Packet, error) {
	if len(opusPacket) == 0 {
		return nil, ErrEmptyOpusPayload
	}
	if p.payloadType > 127 {
		return nil, ErrInvalidPayloadType
	}
	limit := p.MaxPayloadSize
	if limit == 0 {
		limit = DefaultMaxPayloadSize
	}
	if limit > 0 && len(opusPacket) > limit {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrPayloadTooLarge, len(opusPacket), limit)
	}

	// Calculate sample count at 48 kHz (RFC 7587 mandates 48 kHz timestamp clock)
	samples, err := opus.PacketTotalSamples(opusPacket, ClockRate)
	if err != nil {
		return nil, fmt.Errorf("rtp: parse opus packet duration: %w", err)
	}

	pkt := &Packet{
		Version:        2,
		Marker:         marker,
		PayloadType:    p.payloadType,
		SequenceNumber: p.seq,
		Timestamp:      p.timestamp,
		SSRC:           p.ssrc,
		Payload:        opusPacket,
	}

	p.seq++
	p.timestamp += uint32(samples)
	return pkt, nil
}

// SkipTimestamp advances the RTP timestamp by samples (at the 48 kHz clock) without sending a packet. Call it for the
// time that passed while nothing was sent (DTX silence, a muted microphone): the timestamp keeps following real time
// while the sequence numbers stay contiguous, and the next packet should be sent with PacketizeMarker(…, true).
func (p *Packetizer) SkipTimestamp(samples uint32) {
	p.timestamp += samples
}

// DefaultMaxMissingPackets is the upper bound on missing packets reported by Depacketizer
// (100 packets, about 2 seconds of audio at 20ms per frame), preventing excessive PLC iterations.
const DefaultMaxMissingPackets = 100

// Depacketizer unpacks Opus audio frames from incoming RTP packets and detects sequence gaps.
type Depacketizer struct {
	lastSeq    uint16
	haveSeq    bool
	MaxMissing int // Maximum missing packets to report before treating as a stream resync. Defaults to DefaultMaxMissingPackets.
}

// NewDepacketizer creates a new Opus RTP Depacketizer.
func NewDepacketizer() *Depacketizer {
	return &Depacketizer{
		MaxMissing: DefaultMaxMissingPackets,
	}
}

// Depacketize extracts the Opus packet and reports missing packets count since the previous call.
// It properly handles 16-bit sequence number wraparound, bounds missing packets to MaxMissing, and records sequence
// numbers even on empty packets. Feed it packets in arrival order after any jitter buffer: a duplicate
// (ErrDuplicatePacket) or a packet older than one already accepted (ErrLatePacket) comes back without a payload and
// without moving the sequence, and the caller should simply drop it.
func (d *Depacketizer) Depacketize(pkt *Packet) (opusPayload []byte, missingCount int, err error) {
	if pkt == nil {
		return nil, 0, errors.New("rtp: packet is nil")
	}

	maxMissing := d.MaxMissing
	if maxMissing <= 0 {
		maxMissing = DefaultMaxMissingPackets
	}

	if !d.haveSeq {
		d.lastSeq = pkt.SequenceNumber
		d.haveSeq = true
		if len(pkt.Payload) == 0 {
			return nil, 0, ErrEmptyOpusPayload
		}
		return pkt.Payload, 0, nil
	}

	// Signed 16-bit arithmetic correctly handles sequence number wrapping (e.g. 65535 -> 0)
	// and identifies late/out-of-order packets.
	diff := int16(pkt.SequenceNumber - d.lastSeq)

	if diff == 0 {
		return nil, 0, ErrDuplicatePacket
	}
	if diff < 0 {
		// Older than a packet already accepted: lastSeq stays where it is, and the payload is withheld so the
		// caller cannot decode it out of order.
		return nil, 0, ErrLatePacket
	}

	d.lastSeq = pkt.SequenceNumber

	if diff > 1 {
		missing := int(diff - 1)
		if missing > maxMissing {
			// Large jump suggests stream sequence restart or extreme packet loss;
			// resync without flooding decoder with thousands of PLC iterations.
			missingCount = 0
		} else {
			missingCount = missing
		}
	}

	if len(pkt.Payload) == 0 {
		return nil, missingCount, ErrEmptyOpusPayload
	}

	return pkt.Payload, missingCount, nil
}
