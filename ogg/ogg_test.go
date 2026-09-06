package ogg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestRFC3533_LacingRules(t *testing.T) {
	tests := []struct {
		name       string
		packetLen  int
		wantSegLen int
		wantLast   byte
	}{
		{"Empty packet (0 bytes)", 0, 1, 0},
		{"Short packet (10 bytes)", 10, 1, 10},
		{"Single segment boundary (254 bytes)", 254, 1, 254},
		{"Exact 255 bytes (needs terminating 0)", 255, 2, 0},
		{"256 bytes (255 + 1)", 256, 2, 1},
		{"Exact 510 bytes (255*2, needs terminating 0)", 510, 3, 0},
		{"511 bytes (255*2 + 1)", 511, 3, 1},
		{"1000 bytes", 1000, 4, byte(1000 - 3*255)}, // 235
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkt := make([]byte, tc.packetLen)
			for i := range pkt {
				pkt[i] = byte(i % 256)
			}
			segTable, dataLens := oggLacing(pkt)
			if len(segTable) != tc.wantSegLen {
				t.Fatalf("lacing table len mismatch: got %d, want %d (table: %v)", len(segTable), tc.wantSegLen, segTable)
			}
			if segTable[len(segTable)-1] != tc.wantLast {
				t.Fatalf("last lacing byte mismatch: got %d, want %d", segTable[len(segTable)-1], tc.wantLast)
			}

			// Verify total laced length equals packet length
			totalLen := 0
			for _, l := range dataLens {
				totalLen += l
			}
			if totalLen != tc.packetLen {
				t.Fatalf("total data length mismatch: got %d, want %d", totalLen, tc.packetLen)
			}
		})
	}
}

func TestRFC3533_PageRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	const serial uint32 = 0x12345678
	pw := NewPacketWriter(&buf, serial)

	// Write BOS packet
	bosPayload := []byte("first-packet-bos")
	if err := pw.WritePacket(bosPayload, 0, true, false); err != nil {
		t.Fatalf("WritePacket BOS: %v", err)
	}

	// Write regular packet
	regPayload := []byte("second-packet-audio")
	if err := pw.WritePacket(regPayload, 960, false, false); err != nil {
		t.Fatalf("WritePacket audio: %v", err)
	}

	// Write EOS packet
	eosPayload := []byte("third-packet-eos")
	if err := pw.WritePacket(eosPayload, 1920, false, true); err != nil {
		t.Fatalf("WritePacket EOS: %v", err)
	}

	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Read pages back with PageReader
	pr := NewPageReader(&buf)

	// Page 1: BOS
	p1, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage 1: %v", err)
	}
	if !p1.IsBOS() {
		t.Fatal("expected page 1 to have BOS flag")
	}
	if p1.IsEOS() {
		t.Fatal("page 1 should not have EOS flag")
	}
	if p1.BitstreamSerial != serial {
		t.Fatalf("page 1 serial mismatch: got %x, want %x", p1.BitstreamSerial, serial)
	}
	if p1.PageSequence != 0 {
		t.Fatalf("page 1 sequence mismatch: got %d, want 0", p1.PageSequence)
	}
	if !p1.CRCVerified {
		t.Fatal("page 1 CRC should be verified")
	}

	// Page 2: regular
	p2, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage 2: %v", err)
	}
	if p2.IsBOS() || p2.IsEOS() {
		t.Fatal("page 2 should not have BOS or EOS flag")
	}
	if p2.GranulePosition != 960 {
		t.Fatalf("page 2 granule mismatch: got %d, want 960", p2.GranulePosition)
	}
	if p2.PageSequence != 1 {
		t.Fatalf("page 2 sequence mismatch: got %d, want 1", p2.PageSequence)
	}

	// Page 3: EOS
	p3, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage 3: %v", err)
	}
	if !p3.IsEOS() {
		t.Fatal("expected page 3 to have EOS flag")
	}
	if p3.GranulePosition != 1920 {
		t.Fatalf("page 3 granule mismatch: got %d, want 1920", p3.GranulePosition)
	}
	if p3.PageSequence != 2 {
		t.Fatalf("page 3 sequence mismatch: got %d, want 2", p3.PageSequence)
	}

	// EOF
	_, err = pr.ReadPage()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after page 3, got %v", err)
	}
}

func TestRFC3533_MultiPagePacketSpanning(t *testing.T) {
	// A packet larger than 255 * 255 = 65,025 bytes must span across multiple pages
	var buf bytes.Buffer
	const serial uint32 = 0xAABBCCDD
	pw := NewPacketWriter(&buf, serial)

	largePacket := make([]byte, 70000)
	for i := range largePacket {
		largePacket[i] = byte(i * 7)
	}

	if err := pw.WritePacket(largePacket, 5000, true, true); err != nil {
		t.Fatalf("WritePacket large: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verify with PageReader that 2 pages were written and page 2 is marked continued (0x01)
	prPages := NewPageReader(bytes.NewReader(buf.Bytes()))
	pg1, err := prPages.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage 1: %v", err)
	}
	if !pg1.IsBOS() {
		t.Fatal("page 1 should be BOS")
	}
	if pg1.IsContinuedPacket() {
		t.Fatal("page 1 should not be continued")
	}

	pg2, err := prPages.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage 2: %v", err)
	}
	if !pg2.IsContinuedPacket() {
		t.Fatal("page 2 must have continued packet flag (0x01)")
	}
	if !pg2.IsEOS() {
		t.Fatal("page 2 should have EOS flag")
	}

	// Read reassembled packet through PacketReader
	pktReader := NewPacketReader(bytes.NewReader(buf.Bytes()))
	pkt, err := pktReader.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket large: %v", err)
	}

	if len(pkt.Data) != len(largePacket) {
		t.Fatalf("reassembled packet length mismatch: got %d, want %d", len(pkt.Data), len(largePacket))
	}
	if !bytes.Equal(pkt.Data, largePacket) {
		t.Fatal("reassembled packet content does not match original")
	}
	if !pkt.BOS {
		t.Fatal("expected BOS flag on reassembled packet")
	}
	if !pkt.EOS {
		t.Fatal("expected EOS flag on reassembled packet")
	}
	if pkt.GranulePosition != 5000 {
		t.Fatalf("expected granule 5000, got %d", pkt.GranulePosition)
	}
}

func TestRFC3533_CRC32VerificationAndCorruption(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x11223344)
	if err := pw.WritePacket([]byte("test-payload-crc"), 100, true, true); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	raw := buf.Bytes()

	// 1. Valid CRC passes
	pr := NewPageReader(bytes.NewReader(raw))
	page, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage failed on valid CRC: %v", err)
	}
	if !page.CRCVerified {
		t.Fatal("CRCVerified should be true")
	}

	// 2. Corrupted byte in payload triggers CRC error
	corrupted := make([]byte, len(raw))
	copy(corrupted, raw)
	corrupted[len(corrupted)-1] ^= 0xFF // flip bits in payload

	prCorrupt := NewPageReader(bytes.NewReader(corrupted))
	_, err = prCorrupt.ReadPage()
	if err == nil {
		t.Fatal("expected CRC error on corrupted payload, got nil")
	}

	// 3. With VerifyCRC = false, corrupted page is read without CRC error
	prNoVerify := NewPageReader(bytes.NewReader(corrupted))
	prNoVerify.VerifyCRC = false
	pageNoVerify, err := prNoVerify.ReadPage()
	if err != nil {
		t.Fatalf("expected success with VerifyCRC=false, got: %v", err)
	}
	if pageNoVerify.CRCVerified {
		t.Fatal("CRCVerified should be false when VerifyCRC=false")
	}
}

func TestRFC3533_SerialMismatch(t *testing.T) {
	var buf bytes.Buffer
	pw1 := NewPacketWriter(&buf, 100)
	_ = pw1.WritePacket([]byte("pkt-serial-100"), 0, true, false)
	_ = pw1.Flush()

	pw2 := NewPacketWriter(&buf, 200)
	_ = pw2.WritePacket([]byte("pkt-serial-200"), 100, false, true)
	_ = pw2.Flush()

	pr := NewPacketReader(bytes.NewReader(buf.Bytes()))
	// First packet should succeed
	pkt1, err := pr.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket 1: %v", err)
	}
	if pkt1.Serial != 100 {
		t.Fatalf("expected serial 100, got %d", pkt1.Serial)
	}

	// Second packet from different serial should error
	_, err = pr.ReadPacket()
	if !errors.Is(err, ErrSerialMismatch) {
		t.Fatalf("expected ErrSerialMismatch, got: %v", err)
	}
}

func TestRFC3533_InvalidCapturePattern(t *testing.T) {
	badHeader := make([]byte, 27)
	copy(badHeader[:4], "XggS")
	pr := NewPageReader(bytes.NewReader(badHeader))
	_, err := pr.ReadPage()
	if !errors.Is(err, ErrInvalidCapturePattern) {
		t.Fatalf("expected ErrInvalidCapturePattern, got: %v", err)
	}
}

func TestRFC3533_Resynchronization(t *testing.T) {
	// Create two valid pages using PacketWriter
	var buf1, buf2 bytes.Buffer
	pw1 := NewPacketWriter(&buf1, 0x1234)
	if err := pw1.WritePacket([]byte("page-one-payload"), 960, true, false); err != nil {
		t.Fatalf("WritePacket 1: %v", err)
	}
	if err := pw1.Flush(); err != nil {
		t.Fatalf("Flush 1: %v", err)
	}

	pw2 := NewPacketWriter(&buf2, 0x1234)
	if err := pw2.WritePacket([]byte("page-two-payload"), 1920, false, true); err != nil {
		t.Fatalf("WritePacket 2: %v", err)
	}
	if err := pw2.Flush(); err != nil {
		t.Fatalf("Flush 2: %v", err)
	}

	page1Bytes := buf1.Bytes()
	page2Bytes := buf2.Bytes()

	// 1. Inject junk before page 1, between page 1 and page 2, and verify recovery
	var corruptedStream bytes.Buffer
	corruptedStream.Write([]byte("GARBAGE_PREFIX_DATA_CORRUPTION_12345"))
	corruptedStream.Write(page1Bytes)
	corruptedStream.Write([]byte("RANDOM_INTERMEDIATE_NOISE_PACKET_LOSS_BYTES"))
	corruptedStream.Write(page2Bytes)

	pr := NewPageReader(bytes.NewReader(corruptedStream.Bytes()))
	p1, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("failed to resync and read page 1: %v", err)
	}
	if string(p1.SegmentData) != "page-one-payload" {
		t.Fatalf("page 1 data mismatch: got %q, want 'page-one-payload'", string(p1.SegmentData))
	}

	p2, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("failed to resync and read page 2: %v", err)
	}
	if string(p2.SegmentData) != "page-two-payload" {
		t.Fatalf("page 2 data mismatch: got %q, want 'page-two-payload'", string(p2.SegmentData))
	}

	// 2. Resync disabled should fail immediately on corrupted stream prefix
	prNoResync := NewPageReader(bytes.NewReader(corruptedStream.Bytes()))
	prNoResync.Resync = false
	_, err = prNoResync.ReadPage()
	if !errors.Is(err, ErrInvalidCapturePattern) {
		t.Fatalf("expected ErrInvalidCapturePattern when Resync=false, got: %v", err)
	}

	// 3. False capture pattern with invalid version (e.g. version = 99) should be skipped
	var streamWithFalseOggS bytes.Buffer
	streamWithFalseOggS.Write([]byte{'O', 'g', 'g', 'S', 99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	streamWithFalseOggS.Write(page1Bytes)

	prFalseOggS := NewPageReader(bytes.NewReader(streamWithFalseOggS.Bytes()))
	pRecovered, err := prFalseOggS.ReadPage()
	if err != nil {
		t.Fatalf("failed to recover from false OggS header: %v", err)
	}
	if string(pRecovered.SegmentData) != "page-one-payload" {
		t.Fatalf("recovered page data mismatch: got %q", string(pRecovered.SegmentData))
	}

	// 4. Exceeding MaxResync returns ErrResyncFailed
	prLimit := NewPageReader(bytes.NewReader(make([]byte, 5000)))
	prLimit.MaxResync = 1000
	_, err = prLimit.ReadPage()
	if !errors.Is(err, ErrResyncFailed) {
		t.Fatalf("expected ErrResyncFailed when exceeding MaxResync, got: %v", err)
	}
}

func TestRFC3533_MultiPacketPageBatching(t *testing.T) {
	var buf bytes.Buffer
	const serial uint32 = 0x55AA1122
	pw := NewPacketWriter(&buf, serial)
	pw.MaxPageSize = 4000 // Enable multi-packet page batching (RFC 3533 §6)

	// 1. Write BOS packet (MUST be on its own page per RFC 7845 §5.1)
	bosPayload := []byte("OpusHead-BOS-Header")
	if err := pw.WritePacket(bosPayload, 0, true, false); err != nil {
		t.Fatalf("WritePacket BOS: %v", err)
	}

	// 2. Write 10 small packets that should be batched together
	expectedPackets := make([][]byte, 10)
	for i := 0; i < 10; i++ {
		payload := []byte(fmt.Sprintf("audio-frame-%02d", i))
		expectedPackets[i] = payload
		granule := uint64((i + 1) * 960)
		if err := pw.WritePacket(payload, granule, false, false); err != nil {
			t.Fatalf("WritePacket %d: %v", i, err)
		}
	}

	// 3. Write EOS packet (50 bytes)
	eosPayload := []byte("audio-frame-eos")
	if err := pw.WritePacket(eosPayload, 11*960, false, true); err != nil {
		t.Fatalf("WritePacket EOS: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verify page count: BOS (page 0) + 1 Batched Page containing 11 packets (page 1) = 2 pages total!
	prPages := NewPageReader(bytes.NewReader(buf.Bytes()))

	// Page 0: BOS alone
	pg0, err := prPages.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage 0: %v", err)
	}
	if !pg0.IsBOS() {
		t.Fatal("expected page 0 to have BOS flag")
	}
	if len(pg0.SegmentTable) != 1 {
		t.Fatalf("expected 1 segment in BOS page, got %d", len(pg0.SegmentTable))
	}
	if pg0.GranulePosition != 0 {
		t.Fatalf("expected granule 0 for BOS page, got %d", pg0.GranulePosition)
	}

	// Page 1: Batched page with 11 packets
	pg1, err := prPages.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage 1: %v", err)
	}
	if !pg1.IsEOS() {
		t.Fatal("expected page 1 to have EOS flag")
	}
	if len(pg1.SegmentTable) != 11 {
		t.Fatalf("expected 11 segments in batched page, got %d", len(pg1.SegmentTable))
	}
	// Per RFC 3533 §6: page granule position must equal granule position of the last packet
	if pg1.GranulePosition != 11*960 {
		t.Fatalf("expected page granule position %d, got %d", 11*960, pg1.GranulePosition)
	}

	// Ensure no extra pages
	_, err = prPages.ReadPage()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after 2 pages, got: %v", err)
	}

	// 4. Verify that PacketReader reassembles all 12 packets back with exact fidelity
	prPackets := NewPacketReader(bytes.NewReader(buf.Bytes()))
	pktBOS, err := prPackets.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket BOS: %v", err)
	}
	if !pktBOS.BOS || !bytes.Equal(pktBOS.Data, bosPayload) {
		t.Fatalf("BOS packet mismatch")
	}

	for i := 0; i < 10; i++ {
		pkt, err := prPackets.ReadPacket()
		if err != nil {
			t.Fatalf("ReadPacket %d: %v", i, err)
		}
		if !bytes.Equal(pkt.Data, expectedPackets[i]) {
			t.Fatalf("packet %d payload mismatch: got %s, want %s", i, pkt.Data, expectedPackets[i])
		}
	}

	pktEOS, err := prPackets.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket EOS: %v", err)
	}
	if !pktEOS.EOS || !bytes.Equal(pktEOS.Data, eosPayload) {
		t.Fatalf("EOS packet mismatch")
	}
	if !pktEOS.GranuleValid || pktEOS.GranulePosition != 11*960 {
		t.Fatalf("EOS granule mismatch: valid=%v, pos=%d", pktEOS.GranuleValid, pktEOS.GranulePosition)
	}
}

func TestRFC3533_CRC32SliceBy8Equivalence(t *testing.T) {
	// Canonical reference implementation (byte-by-byte)
	refCRC := func(crc uint32, data []byte) uint32 {
		for _, v := range data {
			crc = (crc << 8) ^ crcTable8[0][byte(crc>>24)^v]
		}
		return crc
	}

	// Test across varying lengths from 0 to 2048 bytes (including non-multiples of 8)
	testData := make([]byte, 2048)
	for i := range testData {
		testData[i] = byte(i*37 + 13)
	}

	for length := 0; length <= len(testData); length += 7 {
		chunk := testData[:length]
		want := refCRC(0, chunk)
		got := updateCRC8(0, chunk)
		if got != want {
			t.Fatalf("CRC32 mismatch for length %d: got 0x%08X, want 0x%08X", length, got, want)
		}
	}
}
