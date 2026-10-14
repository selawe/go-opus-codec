package rtp

import (
	"bytes"
	"errors"
	"testing"

	"github.com/selawe/go-opus-codec/opus"
)

func TestRTP_MarshalUnmarshal(t *testing.T) {
	orig := &Packet{
		Version:        2,
		Marker:         true,
		PayloadType:    111, // Common dynamic PT for Opus in WebRTC
		SequenceNumber: 12345,
		Timestamp:      987654321,
		SSRC:           0xAABBCCDD,
		CSRC:           []uint32{0x11111111, 0x22222222},
		Payload:        []byte{0x78, 0x01, 0x02, 0x03},
	}

	raw := orig.Marshal()
	if len(raw) != HeaderLength+8+4 {
		t.Fatalf("expected wire size %d, got %d", HeaderLength+8+4, len(raw))
	}

	var parsed Packet
	if err := parsed.Unmarshal(raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if parsed.Version != 2 || !parsed.Marker || parsed.PayloadType != 111 {
		t.Errorf("header flags mismatch: %+v", parsed)
	}
	if parsed.SequenceNumber != 12345 || parsed.Timestamp != 987654321 || parsed.SSRC != 0xAABBCCDD {
		t.Errorf("header fields mismatch: %+v", parsed)
	}
	if len(parsed.CSRC) != 2 || parsed.CSRC[0] != 0x11111111 || parsed.CSRC[1] != 0x22222222 {
		t.Errorf("CSRC mismatch: %v", parsed.CSRC)
	}
	if !bytes.Equal(parsed.Payload, orig.Payload) {
		t.Errorf("payload mismatch: got %v, want %v", parsed.Payload, orig.Payload)
	}
}

func TestRTP_PacketizerTimestampAdvance(t *testing.T) {
	// 20ms mono Opus packet (config 13: Hybrid mono 20ms -> 960 samples at 48kHz)
	// TOC byte: config 13, stereo=0, code=0 -> (13 << 3) = 104 = 0x68
	dummyOpus20ms := []byte{0x68, 0x00, 0x01, 0x02}

	p := NewPacketizer(0x1234, 111, 100, 1000)

	pkt1, err := p.Packetize(dummyOpus20ms)
	if err != nil {
		t.Fatalf("Packetize pkt1: %v", err)
	}
	if pkt1.SequenceNumber != 100 || pkt1.Timestamp != 1000 {
		t.Errorf("pkt1 seq/ts mismatch: seq=%d, ts=%d", pkt1.SequenceNumber, pkt1.Timestamp)
	}

	// 20 ms at 48 kHz is 960 samples
	pkt2, err := p.Packetize(dummyOpus20ms)
	if err != nil {
		t.Fatalf("Packetize pkt2: %v", err)
	}
	if pkt2.SequenceNumber != 101 || pkt2.Timestamp != 1960 {
		t.Errorf("pkt2 seq/ts mismatch: seq=%d, ts=%d (want 1960)", pkt2.SequenceNumber, pkt2.Timestamp)
	}
}

func TestRTP_DepacketizerLossDetection(t *testing.T) {
	d := NewDepacketizer()

	pkt1 := &Packet{SequenceNumber: 10, Payload: []byte{0x60, 0x01}}
	payload, missing, err := d.Depacketize(pkt1)
	if err != nil || missing != 0 || len(payload) != 2 {
		t.Fatalf("pkt1 failed: err=%v, missing=%d", err, missing)
	}

	// Continuous packet (seq 11)
	pkt2 := &Packet{SequenceNumber: 11, Payload: []byte{0x60, 0x02}}
	_, missing, err = d.Depacketize(pkt2)
	if err != nil || missing != 0 {
		t.Errorf("expected missing=0 for contiguous packet, got %d", missing)
	}

	// Packet gap (seq 14: lost 12, 13 -> missing = 2)
	pkt4 := &Packet{SequenceNumber: 14, Payload: []byte{0x60, 0x04}}
	_, missing, err = d.Depacketize(pkt4)
	if err != nil || missing != 2 {
		t.Errorf("expected missing=2, got %d", missing)
	}
}

func TestRTP_OpusEncodePacketizeDepacketizeDecode(t *testing.T) {
	// Full roundtrip: Encoder -> Packetizer -> Depacketizer -> Decoder
	enc, err := opus.NewEncoder(48000, 1, opus.ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	dec, err := opus.NewDecoder(48000, 1)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	packetizer := NewPacketizer(0x55AA, 111, 1, 0)
	depacketizer := NewDepacketizer()

	// 20ms frame at 48kHz mono = 960 samples
	pcmIn := make([]int16, 960)
	for i := range pcmIn {
		pcmIn[i] = int16(i * 10)
	}

	opusBuf := make([]byte, 1275)
	nBytes, err := enc.Encode(pcmIn, 960, opusBuf)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	rtpPkt, err := packetizer.Packetize(opusBuf[:nBytes])
	if err != nil {
		t.Fatalf("Packetize: %v", err)
	}

	wireBytes := rtpPkt.Marshal()

	var wirePkt Packet
	if err := wirePkt.Unmarshal(wireBytes); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	extractedOpus, missing, err := depacketizer.Depacketize(&wirePkt)
	if err != nil || missing != 0 {
		t.Fatalf("Depacketize: %v, missing=%d", err, missing)
	}

	pcmOut := make([]int16, 960)
	nSamples, err := dec.Decode(extractedOpus, pcmOut, 960, false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if nSamples != 960 {
		t.Errorf("expected 960 decoded samples, got %d", nSamples)
	}
}

func TestRTP_ErrorCases(t *testing.T) {
	var p Packet
	if err := p.Unmarshal([]byte{0x80, 0x00}); !errors.Is(err, ErrPacketTooShort) {
		t.Errorf("expected ErrPacketTooShort, got %v", err)
	}

	// Version != 2
	badVer := make([]byte, 12)
	badVer[0] = 0x40 // Version 1
	if err := p.Unmarshal(badVer); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}

	pktz := NewPacketizer(1, 111, 1, 0)
	if _, err := pktz.Packetize(nil); !errors.Is(err, ErrEmptyOpusPayload) {
		t.Errorf("expected ErrEmptyOpusPayload on nil slice, got %v", err)
	}
}
