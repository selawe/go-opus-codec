package ogg

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// ValidateOpusTags checks that tags are something every reader can take: RFC 7845 section 5.2 comments of the form
// FIELD=value, a field name of one or more characters in 0x20-0x7D other than '=', UTF-8 text in the vendor string and the
// values, and a total size a reader will accept (MaxOpusHeaderPacketSize). The errors match ErrBadOpusTags.
// RewriteOpusTags calls it; BuildOpusTagsPacket does not, so it can still build the malformed packets that tests need.
func ValidateOpusTags(tags OpusTags) error {
	if !utf8.ValidString(tags.Vendor) {
		return fmt.Errorf("%w: vendor string is not valid UTF-8", ErrBadOpusTags)
	}
	size := 8 + 4 + len(tags.Vendor) + 4
	for i, c := range tags.Comments {
		eq := strings.IndexByte(c, '=')
		if eq < 1 {
			return fmt.Errorf("%w: comment %d has no FIELD=value form with a non-empty field name", ErrBadOpusTags, i)
		}
		for j := 0; j < eq; j++ {
			if c[j] < 0x20 || c[j] > 0x7D {
				return fmt.Errorf("%w: comment %d: field name has a character outside 0x20-0x7D at byte %d", ErrBadOpusTags, i, j)
			}
		}
		if !utf8.ValidString(c[eq+1:]) {
			return fmt.Errorf("%w: comment %d: value is not valid UTF-8", ErrBadOpusTags, i)
		}
		size += 4 + len(c)
		if size > MaxOpusHeaderPacketSize {
			return fmt.Errorf("%w: tags exceed %d bytes, more than a reader accepts", ErrBadOpusTags, MaxOpusHeaderPacketSize)
		}
	}
	if size > MaxOpusHeaderPacketSize {
		return fmt.Errorf("%w: tags exceed %d bytes, more than a reader accepts", ErrBadOpusTags, MaxOpusHeaderPacketSize)
	}
	return nil
}

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
//
// The new tags are checked with ValidateOpusTags first, so nothing is written for malformed ones.
//
// In a multiplexed file the tags of the first Opus stream (the first one that starts with OpusHead) are rewritten and
// the pages of every other stream are copied through unchanged. In a chained file only the first link's tags are
// rewritten; later links are copied as they are.
func RewriteOpusTags(r io.Reader, w io.Writer, newTags OpusTags) error {
	if r == nil {
		return errors.New("ogg: reader cannot be nil")
	}
	if w == nil {
		return errors.New("ogg: writer cannot be nil")
	}
	if err := ValidateOpusTags(newTags); err != nil {
		return err // before anything is read or written
	}

	pr := NewPageReader(r)
	pr.VerifyCRC = true
	pr.Resync = false // Never skip corrupted bytes silently in a file rewriting tool

	// Step 1: find the OpusHead page. In a multiplexed file the BOS pages of all streams come first, so pages of
	// other streams may precede it; they are passed through untouched. A non-BOS page before any OpusHead means
	// there is no Opus stream.
	var p0 *Page
	for p0 == nil {
		p, err := pr.ReadPage()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return errTruncatedInput
			}
			return fmt.Errorf("read BOS page: %w", err)
		}
		if !p.IsBOS() {
			return ErrNotOpusOgg
		}
		if len(p.SegmentData) >= 8 && string(p.SegmentData[:8]) == "OpusHead" {
			p0 = p
			break
		}
		if err := writeRawPage(w, p); err != nil {
			return fmt.Errorf("write page of another stream: %w", err)
		}
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

	currSeq := p0.PageSequence + 1 // the rewritten pages continue the numbering after OpusHead
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
		// Granule 0 on the page where the header packet ends; -1 on pages where it continues (RFC 3533).
		granule := uint64(0)
		if nSeg < len(lace) {
			granule = ^uint64(0)
		}
		binary.LittleEndian.PutUint64(hdr[6:14], granule)
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
