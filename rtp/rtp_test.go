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

	raw, err := orig.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
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

	// Late / out-of-order packet (seq 12 arrives late after seq 14)
	pktLate := &Packet{SequenceNumber: 12, Payload: []byte{0x60, 0x02}}
	payload, missing, err = d.Depacketize(pktLate)
	if !errors.Is(err, ErrLatePacket) || payload != nil || missing != 0 {
		t.Errorf("a late packet must come back as ErrLatePacket without payload, got payload=%v missing=%d err=%v", payload, missing, err)
	}

	// Next normal packet after seq 14 is seq 15 (should NOT consider seq 12 as lastSeq)
	pkt15 := &Packet{SequenceNumber: 15, Payload: []byte{0x60, 0x05}}
	_, missing, err = d.Depacketize(pkt15)
	if err != nil || missing != 0 {
		t.Errorf("expected missing=0 for seq 15 after seq 14, got %d", missing)
	}
}

func TestRTP_DepacketizerWraparoundAndBounds(t *testing.T) {
	d := NewDepacketizer()

	// Start at 65535
	pktMax := &Packet{SequenceNumber: 65535, Payload: []byte{0x60, 0x01}}
	_, missing, _ := d.Depacketize(pktMax)
	if missing != 0 {
		t.Errorf("expected missing=0, got %d", missing)
	}

	// Wraparound to seq 0 (contiguous!)
	pktZero := &Packet{SequenceNumber: 0, Payload: []byte{0x60, 0x02}}
	_, missing, _ = d.Depacketize(pktZero)
	if missing != 0 {
		t.Errorf("expected missing=0 on 65535->0 wraparound, got %d", missing)
	}

	// Large gap (exceeding MaxMissing = 100) triggers resync
	pktJump := &Packet{SequenceNumber: 1000, Payload: []byte{0x60, 0x03}}
	_, missing, _ = d.Depacketize(pktJump)
	if missing != 0 {
		t.Errorf("expected missing=0 on large gap resync, got %d", missing)
	}

	// Empty packet sequence recording
	pktEmpty := &Packet{SequenceNumber: 1001, Payload: nil}
	_, _, err := d.Depacketize(pktEmpty)
	if !errors.Is(err, ErrEmptyOpusPayload) {
		t.Errorf("expected ErrEmptyOpusPayload, got %v", err)
	}

	// Packet 1002 following empty packet 1001 should be contiguous (missing = 0)
	pkt1002 := &Packet{SequenceNumber: 1002, Payload: []byte{0x60, 0x04}}
	_, missing, err = d.Depacketize(pkt1002)
	if err != nil || missing != 0 {
		t.Errorf("expected missing=0 after empty packet, got %d (err: %v)", missing, err)
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

	wireBytes, err := rtpPkt.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

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

func TestRTP_MarshalValidationErrors(t *testing.T) {
	// 1. Extension error
	pktExt := &Packet{Extension: true, Payload: []byte{1, 2, 3}}
	if _, err := pktExt.Marshal(); !errors.Is(err, ErrExtensionNotSupported) {
		t.Errorf("expected ErrExtensionNotSupported, got %v", err)
	}

	// 2. Padding error
	pktPad := &Packet{Padding: true, Payload: []byte{1, 2, 3}}
	if _, err := pktPad.Marshal(); !errors.Is(err, ErrPaddingNotSupported) {
		t.Errorf("expected ErrPaddingNotSupported, got %v", err)
	}

	// 3. More than 15 CSRCs
	pktCSRC := &Packet{
		CSRC:    make([]uint32, 16),
		Payload: []byte{1, 2, 3},
	}
	if _, err := pktCSRC.Marshal(); !errors.Is(err, ErrTooManyCSRCs) {
		t.Errorf("expected ErrTooManyCSRCs, got %v", err)
	}

	// 4. PayloadType > 127
	pktPT := &Packet{PayloadType: 128, Payload: []byte{1, 2, 3}}
	if _, err := pktPT.Marshal(); !errors.Is(err, ErrInvalidPayloadType) {
		t.Errorf("expected ErrInvalidPayloadType, got %v", err)
	}

	// 5. Nil packet
	var nilPkt *Packet
	if _, err := nilPkt.Marshal(); err == nil {
		t.Errorf("expected error on nil packet Marshal")
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

// Packets that repeat or arrive after a newer one must not be handed to the decoder: it would play the same audio
// twice, or play it out of order.
func TestRTP_DepacketizerDropsDuplicatesAndLatePackets(t *testing.T) {
	d := NewDepacketizer()
	mk := func(seq uint16) *Packet { return &Packet{SequenceNumber: seq, Payload: []byte{0xFC, byte(seq)}} }

	type step struct {
		seq     uint16
		wantErr error
		missing int
	}
	for i, st := range []step{
		{10, nil, 0},
		{11, nil, 0},
		{11, ErrDuplicatePacket, 0}, // the same packet again
		{9, ErrLatePacket, 0},       // older than the last one
		{10, ErrLatePacket, 0},      // an earlier one, even though it was accepted before
		{13, nil, 1},                // 12 is lost; the late ones above did not disturb the count
		{14, nil, 0},
	} {
		payload, missing, err := d.Depacketize(mk(st.seq))
		if !errors.Is(err, st.wantErr) || missing != st.missing {
			t.Fatalf("step %d (seq %d): err=%v missing=%d, want err=%v missing=%d", i, st.seq, err, missing, st.wantErr, st.missing)
		}
		if st.wantErr != nil && payload != nil {
			t.Fatalf("step %d (seq %d): a dropped packet must not carry a payload, got %v", i, st.seq, payload)
		}
		if st.wantErr == nil && (len(payload) != 2 || payload[1] != byte(st.seq)) {
			t.Fatalf("step %d (seq %d): payload %v", i, st.seq, payload)
		}
	}

	// Across the 16-bit wrap: 65535 then 0 is the next packet, 65535 again is a duplicate of the older one (late).
	w := NewDepacketizer()
	for _, seq := range []uint16{65534, 65535, 0} {
		if _, missing, err := w.Depacketize(mk(seq)); err != nil || missing != 0 {
			t.Fatalf("wrap: seq %d: err=%v missing=%d", seq, err, missing)
		}
	}
	if _, _, err := w.Depacketize(mk(65535)); !errors.Is(err, ErrLatePacket) {
		t.Fatalf("65535 after 0: err=%v, want ErrLatePacket", err)
	}
	// A duplicate of an empty packet is still a duplicate.
	e := NewDepacketizer()
	if _, _, err := e.Depacketize(&Packet{SequenceNumber: 5}); !errors.Is(err, ErrEmptyOpusPayload) {
		t.Fatal(err)
	}
	if _, _, err := e.Depacketize(&Packet{SequenceNumber: 5, Payload: []byte{1}}); !errors.Is(err, ErrDuplicatePacket) {
		t.Fatalf("seq 5 again: err=%v, want ErrDuplicatePacket", err)
	}
}

// RFC 3550 section 5.1: with the P bit set the last octet counts the padding octets including itself, so it is at
// least 1 and at most the number of octets after the header.
func TestRTP_UnmarshalPaddingValidation(t *testing.T) {
	header := []byte{0x80 | 0x20, 111, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1} // version 2, P=1
	build := func(tail ...byte) []byte { return append(append([]byte(nil), header...), tail...) }

	valid := map[string]struct {
		data []byte
		want []byte
	}{
		"2 padding octets":        {build(0xFC, 0x01, 0x00, 0x02), []byte{0xFC, 0x01}},
		"padding is everything":   {build(0x00, 0x00, 0x03), []byte{}},
		"1 octet: just the count": {build(0xFC, 0x01), []byte{0xFC}},
	}
	for name, c := range valid {
		var p Packet
		if err := p.Unmarshal(c.data); err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
			continue
		}
		if !bytes.Equal(p.Payload, c.want) {
			t.Errorf("%s: payload %v, want %v", name, p.Payload, c.want)
		}
	}

	for name, data := range map[string][]byte{
		"padding count 0 (a trailing 0 byte is not padding)": build(0xFC, 0x01, 0x00),
		"P set, nothing after the header":                    build(),
		"padding count larger than the data":                 build(0xFC, 0x09),
		"padding count 255":                                  build(0xFC, 0xFF),
	} {
		var p Packet
		err := p.Unmarshal(data)
		if !errors.Is(err, ErrInvalidPadding) {
			t.Errorf("%s: err = %v, want ErrInvalidPadding", name, err)
		}
		if !errors.Is(err, ErrPacketTooShort) {
			t.Errorf("%s: ErrInvalidPadding must still match ErrPacketTooShort", name)
		}
	}
}

// RFC 7587 section 4.1 (with RFC 3551): the marker bit is set on the first packet after a period of silence (DTX),
// and the timestamp keeps counting through the silence, so the receiver sees the gap in time without a gap in sequence.
func TestRTP_MarkerAndDTXTimestampSkip(t *testing.T) {
	frame := []byte{0xFC, 0x01} // CELT fullband, 20 ms, one frame
	p := NewPacketizer(0x1234, 111, 100, 1000)

	first, err := p.Packetize(frame)
	if err != nil {
		t.Fatal(err)
	}
	if first.Marker {
		t.Error("Packetize must not set the marker bit")
	}

	// 300 ms of DTX silence passes without packets: the timestamp moves on, the sequence does not.
	p.SkipTimestamp(300 * 48)
	after, err := p.PacketizeMarker(frame, true)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Marker {
		t.Error("PacketizeMarker(…, true) must set the marker bit")
	}
	if after.SequenceNumber != first.SequenceNumber+1 {
		t.Errorf("sequence %d after %d: silence must not leave a hole in the sequence numbers", after.SequenceNumber, first.SequenceNumber)
	}
	if want := first.Timestamp + 960 + 300*48; after.Timestamp != want {
		t.Errorf("timestamp %d, want %d (the frame's 960 samples plus the 300 ms skipped)", after.Timestamp, want)
	}

	// The bit survives the wire.
	wire, err := after.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var got Packet
	if err := got.Unmarshal(wire); err != nil {
		t.Fatal(err)
	}
	if !got.Marker || got.Timestamp != after.Timestamp {
		t.Errorf("round trip lost the marker or the timestamp: %+v", got)
	}

	// The next packet is an ordinary one again.
	next, _ := p.Packetize(frame)
	if next.Marker || next.Timestamp != after.Timestamp+960 {
		t.Errorf("packet after the marked one: marker=%v timestamp=%d", next.Marker, next.Timestamp)
	}

	// 32-bit timestamp wraparound in a skip.
	w := NewPacketizer(1, 111, 0, 0xFFFFFF00)
	w.SkipTimestamp(0x200)
	pk, _ := w.Packetize(frame)
	if pk.Timestamp != 0x100 {
		t.Errorf("timestamp after a skip over the wrap: %#x, want 0x100", pk.Timestamp)
	}
}
