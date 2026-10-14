package ogg

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
)

var (
	ErrSerialMismatch   = errors.New("ogg: bitstream serial mismatch")
	ErrBadContinuedPage = errors.New("ogg: page marked continued but no prior packet")
	ErrPacketTooLarge   = errors.New("ogg: packet exceeds maximum allowed size")
)

// DefaultMaxPacketSize is the default upper limit (2 MiB) on reassembled Ogg packets
// to protect against unbounded memory allocation from malicious or corrupted bitstreams.
const DefaultMaxPacketSize = 2 * 1024 * 1024

// maxOggPageSize is the largest possible size of a single Ogg page:
// 27-byte header + 255-byte segment table + 255*255 bytes of payload = 65307 bytes.
// Scanning this many bytes backwards from EOF is guaranteed to contain the start of
// the final page in a well-formed stream. 65536 is used as a round upper bound.
const maxOggPageSize = 65536

// Packet is a reassembled Ogg packet.
type Packet struct {
	Data            []byte
	Serial          uint32
	PageSequenceEnd uint32
	GranulePosition uint64
	GranuleValid    bool
	BOS             bool
	EOS             bool

	// Discontinuity is true when pages were lost (or reordered) immediately before this
	// packet, detected from a gap in the page sequence numbers. Packets that were damaged
	// by the loss are dropped rather than returned, so a decoder should conceal the missing
	// audio (packet loss concealment) before decoding a packet with this flag set.
	Discontinuity bool
}

// PacketReader converts Ogg pages into packets by applying lacing rules.
//
// It assumes a single logical bitstream; if multiple serial numbers
// are present it will return ErrSerialMismatch.
type PacketReader struct {
	pr            *PageReader
	reader        io.Reader // the original reader for seeking
	serial        *uint32
	pending       []byte
	havePending   bool
	pendingBOS    bool
	queue         []*Packet
	MaxPacketSize int

	// Page sequence tracking (RFC 3533 Section 6): lastSeq is the sequence number of the
	// previous page, valid only when haveSeq is set. gap marks that pages were lost and
	// that the next packet returned must be flagged with Discontinuity.
	lastSeq uint32
	haveSeq bool
	gap     bool

	streamEnded bool
	streamCount uint32

	// Stream selection (SelectStream / SelectSerial). With filter set, pages of streams that do not pass it are
	// skipped instead of failing with ErrSerialMismatch, so multiplexed files (Opus next to Theora, say) read.
	filter       func(page *Page) bool
	firstSerial  *uint32             // the first selected stream, restored by a seek
	foreign      map[uint32]struct{} // serials of streams seen and passed over; ignored by seeking and length
	skippedPages int
}

// StreamIndex returns the 0-based index of the logical bitstream currently being read.
func (r *PacketReader) StreamIndex() uint32 {
	return r.streamCount
}

// maxForeignSerials bounds the set of skipped stream serials remembered, so a file announcing endless
// streams cannot grow it without limit.
const maxForeignSerials = 1024

// firstPacketPrefix returns the bytes of the first packet that starts on a (BOS) page, as far as the page holds it.
func firstPacketPrefix(page *Page) []byte {
	n := 0
	for _, seg := range page.SegmentTable {
		n += int(seg)
		if seg < 255 {
			break
		}
	}
	return page.SegmentData[:min(n, len(page.SegmentData))]
}

// SelectStream makes the reader follow only the logical bitstreams whose first packet (the one on their
// BOS page) satisfies accept, skipping every other stream's pages. Without it the reader takes the first
// stream it meets and fails with ErrSerialMismatch on any other serial. In a chained file each new stream
// is chosen the same way. Call it before the first read.
func (r *PacketReader) SelectStream(accept func(firstPacket []byte) bool) {
	if accept == nil {
		r.filter = nil
		return
	}
	r.filter = func(page *Page) bool { return accept(firstPacketPrefix(page)) }
}

// SelectSerial makes the reader follow only the bitstream with the given serial number; see SelectStream.
func (r *PacketReader) SelectSerial(serial uint32) {
	r.filter = func(page *Page) bool { return page.BitstreamSerial == serial }
}

// skipPage records that page belongs to a stream the reader is passing over.
func (r *PacketReader) skipPage(page *Page) {
	r.skippedPages++
	if page.IsBOS() && len(r.foreign) < maxForeignSerials {
		if r.foreign == nil {
			r.foreign = make(map[uint32]struct{})
		}
		r.foreign[page.BitstreamSerial] = struct{}{}
	}
}

// isForeign reports whether a stream with this serial was passed over by the stream selection.
func (r *PacketReader) isForeign(serial uint32) bool {
	_, ok := r.foreign[serial]
	return ok
}

// beginStream starts following the stream with the given serial, forgetting any half-assembled packet.
func (r *PacketReader) beginStream(serial uint32) {
	chained := r.streamEnded
	r.serial = &serial
	if r.firstSerial == nil {
		first := serial
		r.firstSerial = &first
	}
	r.lastSeq = 0
	r.haveSeq = false
	r.pending = nil
	r.havePending = false
	r.pendingBOS = false
	r.gap = false
	r.streamEnded = false
	if chained {
		r.streamCount++
	}
}

// NewPacketReader creates a new PacketReader that demuxes packets from the Ogg stream in r.
func NewPacketReader(r io.Reader) *PacketReader {
	return &PacketReader{
		pr:            NewPageReader(r),
		reader:        r,
		MaxPacketSize: DefaultMaxPacketSize,
	}
}

// SetMaxPacketSize configures the maximum permitted packet size in bytes.
// If a packet being reassembled exceeds this limit, ReadPacket returns ErrPacketTooLarge.
// A size <= 0 disables the limit.
func (r *PacketReader) SetMaxPacketSize(size int) {
	r.MaxPacketSize = size
}

func (r *PacketReader) reset() {
	r.pending = nil
	r.queue = nil
	r.havePending = false
	r.pendingBOS = false
	r.haveSeq = false
	r.gap = false
	r.streamEnded = false
	r.streamCount = 0
	r.serial = nil // the first page after a seek defines the stream again ...
	if r.filter != nil && r.firstSerial != nil {
		first := *r.firstSerial // ... except with stream selection, where the first selected stream stays
		r.serial = &first
	}
}

// keep reading bytes until we find the start of an ogg page
// return the byte position of the start of the page
func (r *PacketReader) findNextPage(seeker io.ReadSeeker, position int64, seekBuffer *bytes.Buffer) (int64, error) {

	seekBuffer.Reset()

	// these bytes are the start of an ogg page
	scanBytes := []byte{'O', 'g', 'g', 'S', 0}

	_, err := seeker.Seek(position, io.SeekStart)
	if err != nil {
		return 0, err
	}

	buffered := bufio.NewReader(seeker)

	// rounds := 0
	for seekBuffer.Len() < maxOggPageSize {
		// rounds += 1

		reader := io.LimitReader(buffered, 4096)

		n, err := seekBuffer.ReadFrom(reader)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return -1, io.EOF
		}

		index := bytes.Index(seekBuffer.Bytes(), scanBytes)

		if index != -1 {
			// fmt.Printf("found page at position %d after %d rounds\n", position+int64(index), rounds)
			return position + int64(index), nil
		}
	}

	return -1, io.EOF
}

func (r *PacketReader) newPageReader(reader io.Reader) *PageReader {
	pr := NewPageReader(reader)
	if r.pr != nil {
		pr.VerifyCRC = r.pr.VerifyCRC
		pr.Resync = r.pr.Resync
		pr.MaxResync = r.pr.MaxResync
	}
	return pr
}

// SetResync enables or disables capture pattern resynchronization.
func (r *PacketReader) SetResync(v bool) {
	if r.pr != nil {
		r.pr.Resync = v
	}
}

// seek to the first page that contains the granule position
// note that the granule position on the page is the position of the last complete packet on that page
// For example, if granulePos == 40000 then we find the page that has the closest granule position below
// 40000, which may be GranulePosition=38000. Then we keep reading packets until we see the last valid packet
// with GranulePosition=38000. The packet we care about must be the next one
func (r *PacketReader) SeekToPage(granulePos uint64) (uint64, error) {
	seeker, ok := r.reader.(io.ReadSeeker)
	if !ok {
		return 0, fmt.Errorf("ogg: underlying reader is not seekable")
	}

	// seek to beginning
	if granulePos == 0 {
		r.reset()
		_, err := seeker.Seek(0, io.SeekStart)
		if err != nil {
			return 0, err
		}
		r.pr = r.newPageReader(seeker)
		// skip headers
		for range 2 {
			_, err := r.ReadPacket()
			if err != nil {
				return 0, err
			}
		}

		return 0, nil
	}

	// Everything up to the final scan only reads: remember where the reader stands (the underlying
	// offset already accounts for the page reader's buffered read-ahead) so that a failed seek
	// leaves it exactly where it was.
	resume, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	total, err := seeker.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	restore := func() { _, _ = seeker.Seek(resume, io.SeekStart) }

	before, found, err := searchPages(seeker, total, granulePos, r.isForeign)
	if err != nil || !found {
		restore()
		if err != nil {
			return 0, err
		}
		// no page reaches the target: it lies beyond the end of the stream
		return 0, fmt.Errorf("ogg: could not find page with granule position %d", granulePos)
	}

	// Scan from the page before the target page (or from the start when the target page is the
	// first audio page). Its granule is the answer's "last" even if its only packet was the tail of
	// one that began earlier and is dropped as an orphan continuation.
	position, last := int64(0), uint64(0)
	if before.valid {
		position, last = before.off, before.granule
	}
	if _, err := seeker.Seek(position, io.SeekStart); err != nil {
		restore()
		return 0, err
	}
	r.reset()
	r.pr = r.newPageReader(seeker)

	for {
		pkt, err := r.ReadPacket()
		if err != nil {
			return 0, err
		}
		if pkt.GranulePosition == math.MaxUint64 {
			continue
		}
		if pkt.GranulePosition >= granulePos {
			// put back in the queue
			r.queue = slices.Insert(r.queue, 0, pkt)
			return last, nil
		}
		last = max(last, pkt.GranulePosition)
	}
}

// seekPage describes one Ogg page found while searching a seekable stream.
type seekPage struct {
	valid   bool
	off     int64
	size    int
	granule uint64 // math.MaxUint64 when no packet ends on the page
	serial  uint32
}

// maxPageBytes is the largest possible Ogg page: a 27-byte header, 255 segment entries and
// 255 segments of 255 bytes.
const maxPageBytes = 27 + 255 + 255*255

// pageAtOrAfter returns the first page that starts at or after off. A candidate "OggS" is only
// accepted if the whole page fits and what follows it is another "OggS" or the end of the
// stream, which rejects a capture pattern that merely occurs inside a payload. The window grows
// only as far as that check needs (usually one page), so a probe costs a few KiB rather than the
// 128 KiB a page reader would buffer. buf is reused between calls and must hold at least
// 2*maxPageBytes+4 bytes.
func pageAtOrAfter(seeker io.ReadSeeker, buf []byte, off, total int64) (seekPage, error) {
	if off >= total {
		return seekPage{}, nil
	}
	if _, err := seeker.Seek(off, io.SeekStart); err != nil {
		return seekPage{}, err
	}
	limit := int(min(int64(len(buf)), total-off))
	have := 0
	// ensure makes the window cover at least n bytes (reading ahead in 4 KiB steps), as far as the
	// stream goes, and reports how many bytes it now holds.
	ensure := func(n int) (int, error) {
		if n <= have {
			return have, nil
		}
		want := min(max(n, have+4096), limit)
		if want > have {
			if _, err := io.ReadFull(seeker, buf[have:want]); err != nil {
				return have, err
			}
			have = want
		}
		return have, nil
	}

	for i := 0; i <= maxPageBytes; i++ {
		n, err := ensure(i + 27)
		if err != nil {
			return seekPage{}, err
		}
		if i+27 > n {
			break // fewer than a header's worth of bytes remain
		}
		if !bytes.HasPrefix(buf[i:n], []byte("OggS\x00")) {
			continue
		}
		nseg := int(buf[i+26])
		if n, err = ensure(i + 27 + nseg); err != nil {
			return seekPage{}, err
		}
		if i+27+nseg > n {
			continue
		}
		body := 0
		for _, v := range buf[i+27 : i+27+nseg] {
			body += int(v)
		}
		end := i + 27 + nseg + body
		last := off+int64(end) == total
		if n, err = ensure(end + 4); err != nil {
			return seekPage{}, err
		}
		if end > n {
			continue
		}
		if !last && (end+4 > n || !bytes.Equal(buf[end:end+4], []byte("OggS"))) {
			continue // not followed by another page: the capture pattern was inside a payload
		}
		return seekPage{valid: true, off: off + int64(i), size: end - i, granule: binary.LittleEndian.Uint64(buf[i+6 : i+14]), serial: binary.LittleEndian.Uint32(buf[i+14 : i+18])}, nil
	}
	return seekPage{}, nil
}

// firstPageWithGranule is pageAtOrAfter that skips pages on which no packet ends and pages of streams that
// were passed over by stream selection (foreign may be nil).
func firstPageWithGranule(seeker io.ReadSeeker, buf []byte, off, total int64, foreign func(serial uint32) bool) (seekPage, error) {
	for {
		p, err := pageAtOrAfter(seeker, buf, off, total)
		if err != nil || !p.valid || (p.granule != math.MaxUint64 && (foreign == nil || !foreign(p.serial))) {
			return p, err
		}
		off = p.off + int64(p.size)
	}
}

// seekScanSlack is the read-ahead of the page reader that finishes a seek (see NewPageReader).
const seekScanSlack = 128 * 1024

// searchPages binary-searches the byte range for the first page whose granule reaches target and
// returns the page before it (the last one below target), if any. found is false when no page
// reaches the target. It assumes granule positions only increase through the stream.
func searchPages(seeker io.ReadSeeker, total int64, target uint64, foreign func(serial uint32) bool) (before seekPage, found bool, err error) {
	buf := make([]byte, 2*maxPageBytes+4)
	lo, hi := int64(0), total
	// Once the target page is known to exist and fewer bytes remain than the final scan's page
	// reader buffers anyway, more probing would only re-read what that scan reads next.
	for lo < hi && (!found || hi-lo > seekScanSlack) {
		mid := lo + (hi-lo)/2
		p, err := firstPageWithGranule(seeker, buf, mid, total, foreign)
		if err != nil {
			return seekPage{}, false, err
		}
		switch {
		case !p.valid:
			hi = mid // nothing with a granule at or after mid
		case p.granule >= target:
			found = true
			hi = mid
		default:
			if !before.valid || p.off > before.off {
				before = p
			}
			lo = p.off + int64(p.size)
		}
	}
	return before, found, nil
}

// LastPageGranule returns the last granule position in the stream.
//
// If the underlying reader is seekable the scan uses a private page reader and
// restores the reader's position afterwards, so packets can keep being read.
// Otherwise it reads through to the end of the stream, which is destructive:
// no more packets can be read afterwards.
func (r *PacketReader) LastPageGranule() (int64, error) {
	seeker, ok := r.reader.(io.ReadSeeker)
	if !ok {
		granule, found, err := r.lastGranuleIn(r.pr)
		if err != nil {
			return 0, err
		}
		if !found {
			return 0, fmt.Errorf("ogg: could not find last page granule")
		}
		return granule, nil
	}

	// The underlying offset already accounts for the page reader's read-ahead
	// buffer, so seeking back to it resumes reading exactly where it stopped.
	resume, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	defer func() { _, _ = seeker.Seek(resume, io.SeekStart) }()

	total, err := seeker.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	// A single Ogg page is at most maxOggPageSize bytes, so backing up that far from EOF is guaranteed to
	// include the start of the final page in a well-formed stream. In a multiplexed file the tail may hold only
	// pages of other streams (one stream can end long before another), so widen the window until a page of the
	// followed stream turns up, or the whole file has been covered.
	for window := int64(maxOggPageSize); ; window *= 8 {
		backup := max(0, total-window)
		position, err := seeker.Seek(backup, io.SeekStart)
		if err != nil {
			return 0, err
		}
		index, err := r.findNextPage(seeker, position, &bytes.Buffer{})
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if err == nil && index != -1 {
			if _, err := seeker.Seek(index, io.SeekStart); err != nil {
				return 0, err
			}
			granule, found, err := r.lastGranuleIn(r.newPageReader(seeker))
			if err != nil {
				return 0, err
			}
			if found {
				return granule, nil
			}
		}
		if backup == 0 {
			return 0, fmt.Errorf("ogg: could not find last page granule")
		}
	}
}

// lastGranuleIn reads pages to the end and returns the granule position of the last page of the followed stream
// that has one, stopping early at that stream's end-of-stream page.
func (r *PacketReader) lastGranuleIn(pages *PageReader) (granule int64, found bool, err error) {
	lastGranule := int64(-1)
	for {
		page, err := pages.ReadPage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, false, fmt.Errorf("ogg: could not find last page granule: %w", err)
		}
		if r.isForeign(page.BitstreamSerial) {
			continue // another stream of a multiplexed file; its granule positions mean something else
		}
		// -1 (all ones) means "no packet ends on this page"; values of 2^63 and above cannot be
		// a sample count and would turn negative in int64, so they are ignored too.
		if page.GranulePosition <= math.MaxInt64 {
			lastGranule = int64(page.GranulePosition)
		}
		if page.IsEOS() && page.GranulePosition <= math.MaxInt64 {
			return int64(page.GranulePosition), true, nil
		}
	}
	return lastGranule, lastGranule >= 0, nil
}

func (r *PacketReader) SetVerifyCRC(v bool) {
	if r.pr != nil {
		r.pr.VerifyCRC = v
	}
}

func (r *PacketReader) ReadPacket() (*Packet, error) {
	if len(r.queue) > 0 {
		pkt := r.queue[0]
		r.queue[0] = nil
		r.queue = r.queue[1:]
		if pkt.EOS {
			r.streamEnded = true
		}
		return pkt, nil
	}

	for {
		page, err := r.pr.ReadPage()
		if err != nil {
			return nil, err
		}

		if r.filter != nil {
			if r.streamEnded || r.serial == nil {
				// Looking for a stream to follow: only the BOS page of an accepted one starts it.
				if !page.IsBOS() || !r.filter(page) {
					r.skipPage(page)
					continue
				}
				r.beginStream(page.BitstreamSerial)
			} else if page.BitstreamSerial != *r.serial {
				r.skipPage(page)
				continue
			}
		} else if r.streamEnded {
			if !page.IsBOS() {
				return nil, fmt.Errorf("%w: chained bitstream must begin with BOS page (got serial %d)", ErrSerialMismatch, page.BitstreamSerial)
			}
			r.beginStream(page.BitstreamSerial)
		} else if r.serial == nil {
			s := page.BitstreamSerial
			r.serial = &s
		} else if page.BitstreamSerial != *r.serial {
			return nil, fmt.Errorf("%w: got=%d want=%d", ErrSerialMismatch, page.BitstreamSerial, *r.serial)
		}

		// A gap in the page sequence means pages were lost (or corrupt pages skipped by
		// resync). Whatever packet was being assembled is damaged: drop it, and skip the
		// continuation of a packet whose start we never saw, instead of splicing the two.
		lost := r.haveSeq && page.PageSequence != r.lastSeq+1
		r.lastSeq, r.haveSeq = page.PageSequence, true
		if lost {
			r.pending = nil
			r.havePending = false
			r.pendingBOS = false
			r.gap = true
		}

		skipOrphan := page.IsContinuedPacket() && !r.havePending
		if skipOrphan && !r.pr.Resync && !lost {
			return nil, ErrBadContinuedPage
		}
		if !page.IsContinuedPacket() {
			// If the page does not continue a previous packet, any prior pending data
			// is invalid per spec; drop it to recover.
			r.pending = r.pending[:0]
			r.havePending = false
			r.pendingBOS = false
		}

		// Walk lacing values to assemble all completed packets on this page.
		body := page.SegmentData
		off := 0
		pagePackets := make([]*Packet, 0, 4)
		pktIndex := 0
		for _, segLenU8 := range page.SegmentTable {
			segLen := int(segLenU8)
			if off+segLen > len(body) {
				return nil, fmt.Errorf("ogg: segment data underrun (seq=%d)", page.PageSequence)
			}
			if skipOrphan {
				off += segLen
				if segLenU8 < 255 {
					skipOrphan = false
				}
				continue
			}
			if r.MaxPacketSize > 0 && len(r.pending)+segLen > r.MaxPacketSize {
				r.pending = nil
				r.havePending = false
				r.pendingBOS = false
				return nil, fmt.Errorf("%w: packet size exceeds limit of %d bytes", ErrPacketTooLarge, r.MaxPacketSize)
			}
			if !r.havePending {
				r.pendingBOS = page.IsBOS() && pktIndex == 0
			}
			r.pending = append(r.pending, body[off:off+segLen]...)
			r.havePending = true
			off += segLen

			if segLenU8 < 255 {
				data := r.pending
				// Detach the backing array so future appends won't overwrite returned data.
				r.pending = nil
				r.havePending = false
				pagePackets = append(pagePackets, &Packet{
					Data:            data,
					Serial:          page.BitstreamSerial,
					PageSequenceEnd: page.PageSequence,
					GranulePosition: page.GranulePosition,
					GranuleValid:    false, // set below for last packet only
					BOS:             r.pendingBOS,
					EOS:             false, // set below for last packet only
				})
				r.pendingBOS = false
				pktIndex++
			}
		}

		if len(pagePackets) == 0 {
			// Page ended with an incomplete packet; keep pending and read next page.
			continue
		}

		granuleValid := page.GranulePosition != math.MaxUint64
		if r.gap {
			pagePackets[0].Discontinuity = true
			r.gap = false
		}
		last := pagePackets[len(pagePackets)-1]
		last.GranuleValid = granuleValid
		last.EOS = page.IsEOS()
		r.queue = append(r.queue, pagePackets...)

		pkt := r.queue[0]
		r.queue[0] = nil
		r.queue = r.queue[1:]
		if pkt.EOS {
			r.streamEnded = true
		}
		return pkt, nil
	}
}
