package ogg

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
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

func TestTruncatedPageReturnsEOF(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	if err := pw.WritePacket([]byte("hello"), 960, true, false); err != nil {
		t.Fatal(err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}

	// Append truncated header (only 10 bytes starting with "OggS")
	buf.Write([]byte("OggS\x00\x00\x00\x00\x00\x00"))

	pr := NewPageReader(&buf)
	p, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if string(p.SegmentData) != "hello" {
		t.Fatalf("unexpected data: %s", p.SegmentData)
	}

	// Second read on truncated header should return io.EOF (clean EOF), NOT ErrResyncFailed
	_, err = pr.ReadPage()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean io.EOF on truncated page, got: %v", err)
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

func TestPageReader_ParsePageDirect_LargePageExceedsBuffer(t *testing.T) {
	var buf bytes.Buffer
	const serial uint32 = 0xABCDEF01
	payload := bytes.Repeat([]byte{0x5A}, 5000)
	pw := NewPacketWriter(&buf, serial)
	if err := pw.WritePacket(payload, 12345, true, true); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Use a bufio buffer far smaller than the page so ReadPage's Peek(totalPageSize)
	// hits bufio.ErrBufferFull and falls back to the parsePageDirect slow path.
	pr := &PageReader{
		r:         bufio.NewReaderSize(bytes.NewReader(buf.Bytes()), 64),
		VerifyCRC: true,
		Resync:    true,
		crcTable:  crcTable8[0],
	}

	page, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page.BitstreamSerial != serial {
		t.Errorf("serial mismatch: got %#x, want %#x", page.BitstreamSerial, serial)
	}
	if !page.IsBOS() || !page.IsEOS() {
		t.Errorf("expected BOS+EOS page, got HeaderType=%#x", page.HeaderType)
	}
	if !page.CRCVerified {
		t.Error("expected CRCVerified true")
	}
	if !bytes.Equal(page.SegmentData, payload) {
		t.Errorf("segment data mismatch: got %d bytes, want %d bytes", len(page.SegmentData), len(payload))
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

func TestPacketReader_MaxPacketSize(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)

	payload := make([]byte, 5000)
	for i := range payload {
		payload[i] = byte(i)
	}

	if err := pw.WritePacket(payload, 960, true, true); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// 1. With MaxPacketSize set smaller than packet (e.g. 2000 bytes)
	pr := NewPacketReader(bytes.NewReader(buf.Bytes()))
	pr.SetMaxPacketSize(2000)
	_, err := pr.ReadPacket()
	if !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("expected ErrPacketTooLarge, got %v", err)
	}

	// 2. With MaxPacketSize set larger (e.g. 10000 bytes)
	pr2 := NewPacketReader(bytes.NewReader(buf.Bytes()))
	pr2.SetMaxPacketSize(10000)
	pkt, err := pr2.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket with limit 10000: %v", err)
	}
	if len(pkt.Data) != len(payload) {
		t.Fatalf("expected %d bytes, got %d", len(payload), len(pkt.Data))
	}
}

func TestSeekToPage_PreservesVerifyCRCAndResync(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	for i := 0; i < 5; i++ {
		_ = pw.WritePacket([]byte(fmt.Sprintf("packet-%d", i)), uint64((i+1)*960), i == 0, i == 4)
	}
	_ = pw.Flush()

	pr := NewPacketReader(bytes.NewReader(buf.Bytes()))
	pr.SetVerifyCRC(false)
	pr.SetResync(false)

	_, err := pr.SeekToPage(0)
	if err != nil {
		t.Fatalf("SeekToPage: %v", err)
	}

	if pr.pr.VerifyCRC != false {
		t.Errorf("SeekToPage clobbered VerifyCRC: got %v, want false", pr.pr.VerifyCRC)
	}
	if pr.pr.Resync != false {
		t.Errorf("SeekToPage clobbered Resync: got %v, want false", pr.pr.Resync)
	}
}

func TestLastPageGranule_ContinuedPage(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	// BOS
	_ = pw.WritePacket([]byte("OpusHead..."), 0, true, false)
	_ = pw.Flush()
	// Write a packet > 65025 bytes so it spans across pages, with EOS
	bigPayload := make([]byte, 70000)
	_ = pw.WritePacket(bigPayload, 48000, false, true)
	_ = pw.Flush()

	pr := NewPacketReader(bytes.NewReader(buf.Bytes()))
	granule, err := pr.LastPageGranule()
	if err != nil {
		t.Fatalf("LastPageGranule failed on continued page: %v", err)
	}
	if granule != 48000 {
		t.Fatalf("expected granule 48000, got %d", granule)
	}
}

func TestSeekToPage_ContinuedPage(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	_ = pw.WritePacket([]byte("OpusHead..."), 0, true, false)
	_ = pw.Flush()
	_ = pw.WritePacket([]byte("OpusTags..."), 0, false, false)
	_ = pw.Flush()

	_ = pw.WritePacket([]byte("audio-1"), 960, false, false)
	_ = pw.Flush()

	// Packet spanning across 2 pages
	bigPayload := make([]byte, 70000)
	_ = pw.WritePacket(bigPayload, 1920, false, false)
	_ = pw.Flush()

	_ = pw.WritePacket([]byte("audio-3"), 2880, false, true)
	_ = pw.Flush()

	pr := NewPacketReader(bytes.NewReader(buf.Bytes()))
	_, err := pr.SeekToPage(2000)
	if err != nil {
		t.Fatalf("SeekToPage failed on continued stream: %v", err)
	}
	pkt, err := pr.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket failed after SeekToPage: %v", err)
	}
	if string(pkt.Data) != "audio-3" {
		t.Fatalf("expected audio-3, got %s", string(pkt.Data))
	}
}

func TestPacketWriter_MaxPagePackets(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 7)
	pw.MaxPageSize = 1 << 20
	pw.MaxPagePackets = 4

	for i := 0; i < 10; i++ {
		if err := pw.WritePacket([]byte{byte(i), 1, 2}, uint64(i+1)*960, false, i == 9); err != nil {
			t.Fatalf("WritePacket %d: %v", i, err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	pr := NewPageReader(bytes.NewReader(buf.Bytes()))
	var counts []int
	for {
		p, err := pr.ReadPage()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPage: %v", err)
		}
		counts = append(counts, len(p.SegmentTable))
	}
	want := []int{4, 4, 2}
	if len(counts) != len(want) {
		t.Fatalf("page packet counts = %v, want %v", counts, want)
	}
	for i := range want {
		if counts[i] != want[i] {
			t.Fatalf("page packet counts = %v, want %v", counts, want)
		}
	}
}

// On a seekable stream LastPageGranule must not disturb the read cursor:
// packets after the call continue exactly where reading stopped.
func TestLastPageGranule_KeepsReadPosition(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	pw.MaxPageSize = 64
	const total = 400
	for i := 0; i < total; i++ {
		payload := []byte(fmt.Sprintf("packet-%04d", i))
		if err := pw.WritePacket(payload, uint64(i+1)*960, i == 0, i == total-1); err != nil {
			t.Fatalf("WritePacket %d: %v", i, err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	pr := NewPacketReader(bytes.NewReader(buf.Bytes()))
	const readBefore = 37
	for i := 0; i < readBefore; i++ {
		if _, err := pr.ReadPacket(); err != nil {
			t.Fatalf("ReadPacket %d: %v", i, err)
		}
	}

	granule, err := pr.LastPageGranule()
	if err != nil {
		t.Fatalf("LastPageGranule: %v", err)
	}
	if want := int64(total) * 960; granule != want {
		t.Fatalf("granule = %d, want %d", granule, want)
	}

	for i := readBefore; i < total; i++ {
		pkt, err := pr.ReadPacket()
		if err != nil {
			t.Fatalf("ReadPacket %d after LastPageGranule: %v", i, err)
		}
		if want := fmt.Sprintf("packet-%04d", i); string(pkt.Data) != want {
			t.Fatalf("packet %d = %q, want %q", i, pkt.Data, want)
		}
	}
	if _, err := pr.ReadPacket(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after last packet, got %v", err)
	}
}

// A stream cut inside a page must be distinguishable from a clean end of stream,
// while still satisfying errors.Is(err, io.EOF) for callers that stop on EOF.
func TestPageReader_TruncationIsDistinguishable(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	for i := 0; i < 2; i++ {
		if err := pw.WritePacket(bytes.Repeat([]byte{byte(i + 1)}, 300), uint64(i+1)*960, i == 0, i == 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}
	full := buf.Bytes()

	pages := NewPageReader(bytes.NewReader(full))
	first, err := pages.ReadPage()
	if err != nil {
		t.Fatal(err)
	}
	firstLen := 27 + len(first.SegmentTable) + len(first.SegmentData)

	// Clean end of stream: plain io.EOF, not a truncation.
	pr := NewPageReader(bytes.NewReader(full))
	for i := 0; i < 2; i++ {
		if _, err := pr.ReadPage(); err != nil {
			t.Fatal(err)
		}
	}
	_, err = pr.ReadPage()
	if !errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("clean end of stream: got %v, want a plain io.EOF", err)
	}

	// Cut inside the fixed header, inside the segment table, and inside the body.
	for name, cut := range map[string]int{
		"header":        firstLen + 10,
		"segment table": firstLen + 27 + 1,
		"body":          len(full) - 5,
	} {
		pr := NewPageReader(bytes.NewReader(full[:cut]))
		if _, err := pr.ReadPage(); err != nil {
			t.Fatalf("%s: first page: %v", name, err)
		}
		_, err := pr.ReadPage()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%s: got %v, want io.ErrUnexpectedEOF", name, err)
		}
		if !errors.Is(err, io.EOF) {
			t.Errorf("%s: got %v, want it to still match io.EOF", name, err)
		}
		if !errors.Is(err, ErrTruncatedPage) {
			t.Errorf("%s: got %v, want ErrTruncatedPage", name, err)
		}
	}
}

// splitPages returns the raw bytes of every page in an Ogg stream.
func splitPages(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	var out [][]byte
	pr := NewPageReader(bytes.NewReader(raw))
	off := 0
	for {
		p, err := pr.ReadPage()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		n := 27 + len(p.SegmentTable) + len(p.SegmentData)
		out = append(out, raw[off:off+n])
		off += n
	}
}

func readAllPackets(t *testing.T, raw []byte) []*Packet {
	t.Helper()
	pr := NewPacketReader(bytes.NewReader(raw))
	var out []*Packet
	for {
		p, err := pr.ReadPacket()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("ReadPacket: %v", err)
		}
		out = append(out, p)
	}
}

// A lost page in the middle of a multi-page packet used to be bridged silently:
// the reader appended the next continuation page to the stale bytes and returned
// one corrupt packet. The damaged packet must be dropped instead.
func TestPacketReader_LostPageDoesNotSpliceContinuedPacket(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	a := bytes.Repeat([]byte{0xA}, 100)
	b := bytes.Repeat([]byte{0xB}, 200000) // spans 4 pages
	c := bytes.Repeat([]byte{0xC}, 100)
	for i, pk := range [][]byte{a, b, c} {
		if err := pw.WritePacket(pk, uint64(i+1)*960, false, i == 2); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}
	pages := splitPages(t, buf.Bytes())
	if len(pages) != 6 {
		t.Fatalf("expected 6 pages, got %d", len(pages))
	}

	// Sanity: the intact stream has no discontinuity.
	for i, p := range readAllPackets(t, buf.Bytes()) {
		if p.Discontinuity {
			t.Errorf("intact stream: packet %d flagged as discontinuity", i)
		}
	}

	damaged := bytes.Join(append(append([][]byte{}, pages[:3]...), pages[4:]...), nil) // drop page seq 3
	for _, resync := range []bool{true, false} {
		pr := NewPacketReader(bytes.NewReader(damaged))
		pr.SetResync(resync)
		var got []*Packet
		for {
			p, err := pr.ReadPacket()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("resync=%v: ReadPacket: %v", resync, err)
			}
			got = append(got, p)
		}
		if len(got) != 2 || !bytes.Equal(got[0].Data, a) || !bytes.Equal(got[1].Data, c) {
			lens := []int{}
			for _, p := range got {
				lens = append(lens, len(p.Data))
			}
			t.Fatalf("resync=%v: got packets with lengths %v, want [A, C] (damaged B dropped)", resync, lens)
		}
		if got[0].Discontinuity {
			t.Errorf("resync=%v: packet before the gap must not be flagged", resync)
		}
		if !got[1].Discontinuity {
			t.Errorf("resync=%v: first packet after the gap must be flagged Discontinuity", resync)
		}
	}
}

func TestPacketReader_LostWholePageIsFlagged(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	for i := 0; i < 5; i++ {
		if err := pw.WritePacket([]byte{byte(i), 1, 2}, uint64(i+1)*960, false, i == 4); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}
	pages := splitPages(t, buf.Bytes())
	damaged := bytes.Join([][]byte{pages[0], pages[1], pages[3], pages[4]}, nil) // drop the packet on page seq 2

	got := readAllPackets(t, damaged)
	if len(got) != 4 {
		t.Fatalf("got %d packets, want 4", len(got))
	}
	for i, p := range got {
		if want := i == 2; p.Discontinuity != want {
			t.Errorf("packet %d: Discontinuity = %v, want %v", i, p.Discontinuity, want)
		}
	}
}

// Seeking lands on an arbitrary page, so the first page after a seek is never a gap.
func TestPacketReader_SeekDoesNotReportDiscontinuity(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x12345678)
	for i := 0; i < 20; i++ {
		if err := pw.WritePacket([]byte{byte(i), 1, 2}, uint64(i+1)*960, i == 0, i == 19); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}
	pr := NewPacketReader(bytes.NewReader(buf.Bytes()))
	for i := 0; i < 3; i++ {
		if _, err := pr.ReadPacket(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pr.SeekToPage(12 * 960); err != nil {
		t.Fatalf("SeekToPage: %v", err)
	}
	for {
		p, err := pr.ReadPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if p.Discontinuity {
			t.Fatalf("packet %v flagged as discontinuity after a seek", p.Data)
		}
	}
}

func onePage(t *testing.T, payload string, eos bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x1234)
	if err := pw.WritePacket([]byte(payload), 960, true, eos); err != nil {
		t.Fatal(err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Junk after the last page (an ID3/APE tag, zero padding) is the end of the stream,
// not a resynchronisation failure.
func TestPageReader_TrailingGarbageIsEOF(t *testing.T) {
	data := append(onePage(t, "last", true), []byte("ID3-tag-or-zero-padding\x00\x00\x00\x00\x00")...)
	pr := NewPageReader(bytes.NewReader(data))
	if _, err := pr.ReadPage(); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.ReadPage(); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

// Input that is junk from the start must still fail loudly, not look like an empty stream.
func TestPageReader_PureGarbageStillFails(t *testing.T) {
	pr := NewPageReader(bytes.NewReader(bytes.Repeat([]byte("not ogg at all "), 10)))
	if _, err := pr.ReadPage(); !errors.Is(err, ErrResyncFailed) {
		t.Fatalf("err = %v, want ErrResyncFailed", err)
	}
}

// A false "OggS" header claiming a huge body must not hide the real page that follows it.
func TestPageReader_TruncatedCandidateDoesNotHideRealPage(t *testing.T) {
	fake := append([]byte("OggS\x00\x00"), make([]byte, 20)...) // header type .. serial .. crc
	fake = append(fake, 255)                                    // 255 segments
	fake = append(fake, bytes.Repeat([]byte{255}, 255)...)      // claiming ~65 KB of body
	data := append(fake, onePage(t, "real", false)...)

	pr := NewPageReader(bytes.NewReader(data))
	p, err := pr.ReadPage()
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if string(p.SegmentData) != "real" {
		t.Fatalf("got %q, want the real page", p.SegmentData)
	}
}

// Crafted back-to-back candidates must hit the work bound rather than checksum ~64 KB each.
func TestPageReader_ResyncCRCWorkIsBounded(t *testing.T) {
	cand := append([]byte("OggS\x00\x00"), make([]byte, 20)...)
	cand = append(cand, 255)
	cand = append(cand, bytes.Repeat([]byte{255}, 255)...)
	// Candidates 283 bytes apart, each claiming 65 KB that the following candidates "fill".
	blob := bytes.Repeat(cand, 4000)
	blob = append(blob, make([]byte, 70000)...)

	pr := NewPageReader(bytes.NewReader(blob))
	pr.MaxResync = 1 << 30 // isolate the CRC bound from the byte bound
	_, err := pr.ReadPage()
	if !errors.Is(err, ErrResyncFailed) || !strings.Contains(err.Error(), "too many false page candidates") {
		t.Fatalf("err = %v, want ErrResyncFailed from the work bound", err)
	}
}
