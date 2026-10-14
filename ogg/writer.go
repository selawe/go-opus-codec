package ogg

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

var (
	// ErrWriterClosed is returned by a PacketWriter used after Close.
	ErrWriterClosed = errors.New("ogg: write after Close")
	// ErrWriteAfterEOS is returned when a packet is written after one flagged as the end of the
	// stream: a logical bitstream ends at its EOS page.
	ErrWriteAfterEOS = errors.New("ogg: write after the end-of-stream packet")
	// ErrMissingEOS is returned by Close when no packet was flagged as the end of the stream and
	// there was no pending page left to flag, so the stream cannot be terminated properly.
	ErrMissingEOS = errors.New("ogg: stream closed without an end-of-stream packet")
)

// PacketWriter writes Ogg packets as pages using the lacing scheme (RFC 3533).
//
// This is a minimal single-stream writer. Write the OpusHead and OpusTags packets with
// WriteOpusHeaders (it keeps the RFC 7845 section 3 page layout), flag the last audio packet as the
// end of the stream, and call Close. The first write error is sticky: every later call returns it.
type PacketWriter struct {
	bw *bufio.Writer

	serial uint32
	seq    uint32

	// MaxPageSize controls multi-packet page batching (RFC 3533 §6).
	// If MaxPageSize > 0, small packets are buffered together into a single page
	// until MaxPageSize is reached, 255 segments are accumulated, or Flush/FlushPage/EOS occurs.
	// If MaxPageSize <= 0 (default), each packet is emitted on its own page (1:1 mode).
	MaxPageSize int

	// MaxPagePackets, if > 0, also ends a batched page once it holds this many
	// packets. Use it to bound page duration (for example one second of audio),
	// which keeps seeking granular at low bitrates. It has no effect when
	// MaxPageSize <= 0.
	MaxPagePackets int

	pageHeaderType uint8
	pageGranule    uint64
	pageSegTable   []byte
	pageData       []byte
	hasPendingPage bool
	pagePackets    int

	laceBuf    []byte
	pageHeader [27]byte

	err      error // first write error, returned by every later call
	eos      bool  // a packet flagged as the end of the stream has been written
	closed   bool
	closeErr error
}

// NewPacketWriter creates a new PacketWriter that serializes packets into Ogg pages using the given bitstream serial.
func NewPacketWriter(w io.Writer, serial uint32) *PacketWriter {
	bw, ok := w.(*bufio.Writer)
	if !ok {
		bw = bufio.NewWriterSize(w, 1<<20)
	}

	return &PacketWriter{
		bw:           bw,
		serial:       serial,
		seq:          0,
		pageSegTable: make([]byte, 0, 255),
		pageData:     make([]byte, 0, 4096),
		laceBuf:      make([]byte, 0, 255),
	}
}

// FlushPage forces any buffered packets in the current page to be written out.
func (pw *PacketWriter) FlushPage() error {
	if pw == nil {
		return nil
	}
	if pw.err != nil {
		return pw.err
	}
	if !pw.hasPendingPage || len(pw.pageSegTable) == 0 {
		return nil
	}
	err := pw.writePage(pw.pageHeaderType, pw.pageGranule, pw.pageSegTable, pw.pageData)
	if err != nil {
		return err
	}
	pw.seq++
	pw.pageHeaderType = 0
	pw.pageGranule = 0
	pw.pageSegTable = pw.pageSegTable[:0]
	pw.pageData = pw.pageData[:0]
	pw.hasPendingPage = false
	pw.pagePackets = 0
	return nil
}

// Flush writes any pending batched page and flushes the underlying buffered writer.
func (pw *PacketWriter) Flush() error {
	if pw == nil {
		return nil
	}
	if err := pw.FlushPage(); err != nil {
		return err
	}
	if pw.bw == nil {
		return nil
	}
	if err := pw.bw.Flush(); err != nil {
		pw.err = err
		return err
	}
	return nil
}

// WriteOpusHeaders writes the OpusHead and OpusTags packets with the page layout RFC 7845 section 3
// requires: OpusHead alone on the first (BOS) page, OpusTags ending its own page, so that the first
// audio packet starts on a fresh page even when batching (MaxPageSize) is on. Call it once, first.
func (pw *PacketWriter) WriteOpusHeaders(head OpusHead, tags OpusTags) error {
	headPkt, err := BuildOpusHeadPacket(head)
	if err != nil {
		return err
	}
	tagsPkt, err := BuildOpusTagsPacket(tags)
	if err != nil {
		return err
	}
	for i, pkt := range [][]byte{headPkt, tagsPkt} {
		if err := pw.WritePacket(pkt, 0, i == 0, false); err != nil {
			return err
		}
		if err := pw.FlushPage(); err != nil {
			return err
		}
	}
	return nil
}

// Close ends the stream: a page still buffered by batching is flagged as the end of the stream and
// written, and the buffered output is flushed (the underlying writer is not closed). It returns
// ErrMissingEOS if no end-of-stream packet was written and nothing was pending to flag; the same
// error is returned by every later call, and writes after Close fail with ErrWriterClosed.
func (pw *PacketWriter) Close() error {
	if pw == nil {
		return nil
	}
	if pw.closed {
		return pw.closeErr
	}
	pw.closed = true
	pw.closeErr = pw.finish()
	return pw.closeErr
}

func (pw *PacketWriter) finish() error {
	if pw.err != nil {
		return pw.err
	}
	if !pw.eos && pw.hasPendingPage {
		pw.pageHeaderType |= 0x04
		pw.eos = true
	}
	if err := pw.Flush(); err != nil {
		return err
	}
	if !pw.eos {
		return ErrMissingEOS
	}
	return nil
}

// lacing returns the lacing values of packet: 255-byte segments, ended by one shorter than 255
// (a zero when the length is a multiple of 255). The slice is reused by the next call.
func (pw *PacketWriter) lacing(packet []byte) []byte {
	pw.laceBuf = pw.laceBuf[:0]
	remaining := len(packet)
	for remaining >= 255 {
		pw.laceBuf = append(pw.laceBuf, 255)
		remaining -= 255
	}
	pw.laceBuf = append(pw.laceBuf, byte(remaining))
	return pw.laceBuf
}

// WritePacket writes a single logical Ogg packet.
//
// If MaxPageSize > 0, small packets are batched together into a single page
// to reduce framing overhead per RFC 3533 §6.
//
// If the packet is too large for one page, it will be continued across pages.
// For continued packets, only the final page carries the provided granulePos;
// earlier pages use granulePos = -1 (0xFFFFFFFFFFFFFFFF).
func (pw *PacketWriter) WritePacket(packet []byte, granulePos uint64, bos bool, eos bool) error {
	if pw.err != nil {
		return pw.err
	}
	if pw.closed {
		return ErrWriterClosed
	}
	if pw.eos {
		return ErrWriteAfterEOS
	}
	if err := pw.writePacket(packet, granulePos, bos, eos); err != nil {
		return err
	}
	pw.eos = eos
	return nil
}

func (pw *PacketWriter) writePacket(packet []byte, granulePos uint64, bos bool, eos bool) error {
	segTable := pw.lacing(packet)

	// If batching is disabled (MaxPageSize <= 0), or if packet spans across pages (> 255 segments):
	if pw.MaxPageSize <= 0 || len(segTable) > 255 {
		if pw.hasPendingPage {
			if err := pw.FlushPage(); err != nil {
				return err
			}
		}
		return pw.writeSpanningPacket(packet, granulePos, bos, eos)
	}

	// Batching is enabled (MaxPageSize > 0) and len(segTable) <= 255:
	// 1. BOS packets MUST be placed alone on the first page per RFC 7845 §5.1.
	if bos {
		if pw.hasPendingPage {
			if err := pw.FlushPage(); err != nil {
				return err
			}
		}
		pw.pageHeaderType = 0x02 // BOS
		if eos {
			pw.pageHeaderType |= 0x04 // EOS
		}
		pw.pageGranule = granulePos
		pw.pageSegTable = append(pw.pageSegTable, segTable...)
		pw.pageData = append(pw.pageData, packet...)
		pw.hasPendingPage = true
		return pw.FlushPage()
	}

	// 2. Regular packet: check if it fits in the current page buffer.
	fits := (len(pw.pageSegTable)+len(segTable) <= 255) &&
		(len(pw.pageData)+len(packet) <= pw.MaxPageSize)

	if !fits && pw.hasPendingPage {
		if err := pw.FlushPage(); err != nil {
			return err
		}
	}

	pw.pageSegTable = append(pw.pageSegTable, segTable...)
	pw.pageData = append(pw.pageData, packet...)
	pw.pageGranule = granulePos
	pw.hasPendingPage = true
	pw.pagePackets++

	if eos {
		pw.pageHeaderType |= 0x04
		return pw.FlushPage()
	}

	if len(pw.pageData) >= pw.MaxPageSize || len(pw.pageSegTable) >= 255 ||
		(pw.MaxPagePackets > 0 && pw.pagePackets >= pw.MaxPagePackets) {
		return pw.FlushPage()
	}

	return nil
}

func (pw *PacketWriter) writeSpanningPacket(packet []byte, granulePos uint64, bos bool, eos bool) error {
	lace := pw.lacing(packet)
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

		// Determine header type.
		headerType := uint8(0)
		if pageIndex > 0 {
			headerType |= 0x01 // continued packet
		}
		if bos && pageIndex == 0 {
			headerType |= 0x02
		}
		isLastPage := nSeg == len(lace)
		if eos && isLastPage {
			headerType |= 0x04
		}

		gp := granulePos
		if !isLastPage {
			gp = math.MaxUint64
		}

		pageData := packet[dataOff : dataOff+dataLen]
		if err := pw.writePage(headerType, gp, segTable, pageData); err != nil {
			return err
		}

		pw.seq++
		pageIndex++
		dataOff += dataLen
		lace = lace[nSeg:]
	}

	return nil
}

func oggLacing(packet []byte) (segTable []byte, segDataLens []int) {
	// Generate lacing values (255-byte segments). Packet ends when a segment < 255.
	remaining := len(packet)
	for remaining >= 255 {
		segTable = append(segTable, 255)
		segDataLens = append(segDataLens, 255)
		remaining -= 255
	}
	segTable = append(segTable, byte(remaining))
	segDataLens = append(segDataLens, remaining)
	return segTable, segDataLens
}

func (pw *PacketWriter) writePage(headerType uint8, granulePos uint64, segTable []byte, segData []byte) error {
	header := pw.pageHeader[:]
	copy(header[0:4], []byte("OggS"))
	header[4] = 0
	header[5] = headerType
	binary.LittleEndian.PutUint64(header[6:14], granulePos)
	binary.LittleEndian.PutUint32(header[14:18], pw.serial)
	binary.LittleEndian.PutUint32(header[18:22], pw.seq)
	binary.LittleEndian.PutUint32(header[22:26], 0) // checksum placeholder
	header[26] = byte(len(segTable))

	crc := oggCRC3(header, segTable, segData)
	binary.LittleEndian.PutUint32(header[22:26], crc)

	// A failed write leaves a partial page in the buffer; nothing after it can be valid, so the
	// error is remembered and returned by every later call.
	if _, err := pw.bw.Write(header); err != nil {
		pw.err = err
		return err
	}
	if _, err := pw.bw.Write(segTable); err != nil {
		pw.err = err
		return err
	}
	if len(segData) > 0 {
		if _, err := pw.bw.Write(segData); err != nil {
			pw.err = err
			return err
		}
	}
	return nil
}
