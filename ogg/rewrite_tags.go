package ogg

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// truncatedInputError is what RewriteOpusTags returns when its input ends early. It matches ErrTruncatedPage and
// io.ErrUnexpectedEOF but, unlike ErrTruncatedPage (which wraps io.EOF for the page reader's benefit), NOT io.EOF:
// callers routinely read errors.Is(err, io.EOF) as "finished", and a cut-off file is not.
type truncatedInputError struct{}

func (truncatedInputError) Error() string {
	return "ogg: input ends before the end of the stream (truncated)"
}
func (truncatedInputError) Is(target error) bool { return target == ErrTruncatedPage }
func (truncatedInputError) Unwrap() error        { return io.ErrUnexpectedEOF }

var errTruncatedInput error = truncatedInputError{}

// RewriteOpusTags streams an Ogg Opus file from r to w, replacing its OpusTags
// metadata comments with newTags without decoding or re-encoding any audio packets.
// All audio data and granule positions are preserved bit-for-bit, while page sequence
// numbers and page checksums are updated to maintain bitstream validity (RFC 3533 & RFC 7845).
func RewriteOpusTags(r io.Reader, w io.Writer, newTags OpusTags) error {
	if r == nil {
		return errors.New("ogg: reader cannot be nil")
	}
	if w == nil {
		return errors.New("ogg: writer cannot be nil")
	}

	pr := NewPageReader(r)
	pr.VerifyCRC = true
	pr.Resync = false // Never skip corrupted bytes silently in a file rewriting tool

	// Step 1: Read the first page (must be BOS and contain OpusHead).
	p0, err := pr.ReadPage()
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return errTruncatedInput
		}
		return fmt.Errorf("read BOS page: %w", err)
	}

	if !p0.IsBOS() || len(p0.SegmentData) < 8 || string(p0.SegmentData[:8]) != "OpusHead" {
		return ErrNotOpusOgg
	}
	serial := p0.BitstreamSerial
	expectedInputSeq := p0.PageSequence + 1

	// Write the OpusHead BOS page unchanged.
	if err := writeRawPage(w, p0); err != nil {
		return fmt.Errorf("write BOS page: %w", err)
	}

	// Step 2: Skip old OpusTags page(s).
	// Per RFC 7845 §3, OpusTags follows OpusHead immediately and terminates its own page.
	firstTagPage := true
	for {
		p, err := pr.ReadPage()
		if err != nil {
			// A stream that stops after OpusHead is truncated too (its OpusTags never arrived).
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, ErrTruncatedPage) {
				return errTruncatedInput
			}
			return fmt.Errorf("read old OpusTags: %w", err)
		}
		if p.BitstreamSerial != serial {
			// Foreign multiplexed page, write through
			if err := writeRawPage(w, p); err != nil {
				return err
			}
			continue
		}

		if p.PageSequence != expectedInputSeq {
			return fmt.Errorf("ogg: page sequence gap in input stream: got %d, want %d", p.PageSequence, expectedInputSeq)
		}
		expectedInputSeq++

		if firstTagPage {
			if len(p.SegmentData) < 8 || string(p.SegmentData[:8]) != "OpusTags" {
				return fmt.Errorf("%w: second page is not OpusTags", ErrHeaderSequence)
			}
			firstTagPage = false
		}

		// Check if this page ends the OpusTags packet.
		// A packet ends when its final lacing segment is < 255.
		packetEnded := false
		for _, seg := range p.SegmentTable {
			if seg < 255 {
				packetEnded = true
				break
			}
		}
		if packetEnded {
			break
		}
	}

	// Step 3: Write new OpusTags packet as Ogg page(s).
	tagsPkt, err := BuildOpusTagsPacket(newTags)
	if err != nil {
		return fmt.Errorf("build opus tags packet: %w", err)
	}

	currSeq := uint32(1)
	if err := writeSpanningTags(w, serial, &currSeq, tagsPkt); err != nil {
		return fmt.Errorf("write new OpusTags: %w", err)
	}

	// Step 4: Stream and resequence all subsequent audio pages.
	sawEOS := false
	for {
		p, err := pr.ReadPage()
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, ErrTruncatedPage) {
				return errTruncatedInput
			}
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read audio page: %w", err)
		}

		if p.BitstreamSerial == serial {
			if p.PageSequence != expectedInputSeq {
				return fmt.Errorf("ogg: page sequence gap in input stream: got %d, want %d", p.PageSequence, expectedInputSeq)
			}
			expectedInputSeq++

			if p.IsEOS() {
				sawEOS = true
			}

			// Resequence page
			p.PageSequence = currSeq
			currSeq++
			if err := writeResequencedPage(w, p); err != nil {
				return fmt.Errorf("write audio page: %w", err)
			}
		} else {
			// Multiplexed foreign stream page, write as-is
			if err := writeRawPage(w, p); err != nil {
				return err
			}
		}
	}

	if !sawEOS {
		return fmt.Errorf("%w: input stream ended without EOS page", ErrMissingEOS)
	}

	return nil
}

func writeRawPage(w io.Writer, p *Page) error {
	var hdr [27]byte
	copy(hdr[0:4], "OggS")
	hdr[4] = p.Version
	hdr[5] = p.HeaderType
	binary.LittleEndian.PutUint64(hdr[6:14], p.GranulePosition)
	binary.LittleEndian.PutUint32(hdr[14:18], p.BitstreamSerial)
	binary.LittleEndian.PutUint32(hdr[18:22], p.PageSequence)
	binary.LittleEndian.PutUint32(hdr[22:26], p.Checksum)
	hdr[26] = byte(len(p.SegmentTable))

	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(p.SegmentTable); err != nil {
		return err
	}
	if len(p.SegmentData) > 0 {
		if _, err := w.Write(p.SegmentData); err != nil {
			return err
		}
	}
	return nil
}

func writeResequencedPage(w io.Writer, p *Page) error {
	var hdr [27]byte
	copy(hdr[0:4], "OggS")
	hdr[4] = p.Version
	hdr[5] = p.HeaderType
	binary.LittleEndian.PutUint64(hdr[6:14], p.GranulePosition)
	binary.LittleEndian.PutUint32(hdr[14:18], p.BitstreamSerial)
	binary.LittleEndian.PutUint32(hdr[18:22], p.PageSequence)
	binary.LittleEndian.PutUint32(hdr[22:26], 0) // zero before CRC
	hdr[26] = byte(len(p.SegmentTable))

	crc := oggCRC3(hdr[:], p.SegmentTable, p.SegmentData)
	binary.LittleEndian.PutUint32(hdr[22:26], crc)

	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(p.SegmentTable); err != nil {
		return err
	}
	if len(p.SegmentData) > 0 {
		if _, err := w.Write(p.SegmentData); err != nil {
			return err
		}
	}
	return nil
}

func writeSpanningTags(w io.Writer, serial uint32, seq *uint32, packet []byte) error {
	lace, _ := oggLacing(packet)
	dataOff := 0
	pageIndex := 0

	for len(lace) > 0 {
		nSeg := len(lace)
		if nSeg > 255 {
			nSeg = 255
		}
		segTable := lace[:nSeg]
		dataLen := 0
		for i := 0; i < nSeg; i++ {
			dataLen += int(segTable[i])
		}

		headerType := uint8(0)
		if pageIndex > 0 {
			headerType |= 0x01 // continued packet
		}

		pageData := packet[dataOff : dataOff+dataLen]

		var hdr [27]byte
		copy(hdr[0:4], "OggS")
		hdr[4] = 0
		hdr[5] = headerType
		binary.LittleEndian.PutUint64(hdr[6:14], 0) // granule 0 for header
		binary.LittleEndian.PutUint32(hdr[14:18], serial)
		binary.LittleEndian.PutUint32(hdr[18:22], *seq)
		binary.LittleEndian.PutUint32(hdr[22:26], 0)
		hdr[26] = byte(len(segTable))

		crc := oggCRC3(hdr[:], segTable, pageData)
		binary.LittleEndian.PutUint32(hdr[22:26], crc)

		if _, err := w.Write(hdr[:]); err != nil {
			return err
		}
		if _, err := w.Write(segTable); err != nil {
			return err
		}
		if len(pageData) > 0 {
			if _, err := w.Write(pageData); err != nil {
				return err
			}
		}

		*seq++
		pageIndex++
		dataOff += dataLen
		lace = lace[nSeg:]
	}
	return nil
}
