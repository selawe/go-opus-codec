package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotOpusOgg     = errors.New("ogg: not an Ogg Opus stream")
	ErrBadOpusHead    = errors.New("ogg: invalid OpusHead")
	ErrBadOpusTags    = errors.New("ogg: invalid OpusTags")
	ErrUnexpectedBOS  = errors.New("ogg: unexpected BOS placement")
	ErrUnexpectedEOS  = errors.New("ogg: unexpected EOS placement")
	ErrHeaderSequence = errors.New("ogg: missing OpusHead/OpusTags")
)

var (
	opusHeadMagic = []byte("OpusHead")
	opusTagsMagic = []byte("OpusTags")
)

// OpusHead is the Opus identification header (RFC 7845).
//
// Opus is always decoded at 48 kHz; granule positions are in 48 kHz samples.
// InputSampleRate is informational.
type OpusHead struct {
	Version              uint8
	Channels             uint8
	PreSkip              uint16
	InputSampleRate      uint32
	OutputGainQ8         int16
	ChannelMappingFamily uint8

	// Present when ChannelMappingFamily != 0
	StreamCount        uint8
	CoupledStreamCount uint8
	ChannelMapping     []uint8
}

// OpusTags is the Opus comment header. This parser is minimal.
type OpusTags struct {
	Vendor   string
	Comments []string
}

// Get returns the value of the first comment matching key (case-insensitive).
// For example, tags.Get("TITLE") returns "My Song" from "TITLE=My Song".
// Returns empty string if key is not found.
func (t OpusTags) Get(key string) string {
	prefix := strings.ToUpper(key) + "="
	for _, c := range t.Comments {
		if len(c) >= len(prefix) && strings.EqualFold(c[:len(prefix)], prefix) {
			return c[len(prefix):]
		}
	}
	return ""
}

// GetAll returns all comment values matching key (case-insensitive).
func (t OpusTags) GetAll(key string) []string {
	prefix := strings.ToUpper(key) + "="
	var matches []string
	for _, c := range t.Comments {
		if len(c) >= len(prefix) && strings.EqualFold(c[:len(prefix)], prefix) {
			matches = append(matches, c[len(prefix):])
		}
	}
	return matches
}

// OpusAudioPacket represents an extracted Opus audio payload with container metadata.
type OpusAudioPacket struct {
	Data         []byte // Encoded Opus packet bytes
	GranulePos   uint64 // Ogg granule position
	GranuleValid bool   // True if the packet finishes on a page with a valid granule position
	EOS          bool   // True if this is the final packet (End of Stream)
	PageSequence uint32 // Ogg page sequence number

	// Discontinuity is true when pages were lost right before this packet (a gap in the page
	// sequence numbers). Damaged packets are dropped, never returned, so the audio before this
	// packet is missing; conceal it (for example with a nil-packet decode) before decoding.
	Discontinuity bool

	// NewStream is true if this packet is the first audio packet of a new chained logical bitstream.
	NewStream bool

	// StreamIndex is the 0-based index of the logical bitstream in a chained Ogg file.
	StreamIndex uint32
}

// OpusReader reads an Ogg Opus file/stream and yields Opus audio packets.
type OpusReader struct {
	pr *PacketReader

	Head OpusHead
	Tags OpusTags

	headRead bool
	tagsRead bool

	streamIndex uint32

	cachedTotalSamples int64
	cachedTotalErr     error
	cachedTotalOnce    sync.Once
}

// StreamIndex returns the 0-based index of the current logical bitstream in a chained stream.
func (r *OpusReader) StreamIndex() uint32 {
	return r.streamIndex
}

// OpusSampleRateHz is the Opus decoding sample rate (RFC 7845).
const OpusSampleRateHz = 48000

// MaxOpusPacketSize is the maximum permitted size (64 KiB) for an Opus audio packet in Ogg Opus (RFC 7845 / RFC 6716).
// A single Opus packet can contain up to 48 frames with maximum 120 ms audio (~61,200 bytes) plus header/padding.
const MaxOpusPacketSize = 64 * 1024

// MaxOpusHeaderPacketSize is the maximum permitted size (16 MiB) for the OpusHead and OpusTags header
// packets. RFC 7845 sets no limit on OpusTags, and embedded cover art (METADATA_BLOCK_PICTURE)
// routinely makes it far larger than an audio packet, so headers get a more generous cap than
// MaxOpusPacketSize. Audio packets are still limited to MaxOpusPacketSize.
const MaxOpusHeaderPacketSize = 16 * 1024 * 1024

// NewOpusReader creates a new OpusReader reading from r, parsing the mandatory OpusHead and OpusTags headers.
func NewOpusReader(r io.Reader) (*OpusReader, error) {
	return NewOpusReaderVerifyCRC(r, true)
}

// NewOpusReaderVerifyCRC is NewOpusReader with control over checksum verification from the
// very first page. Calling SetVerifyCRC afterwards is too late for the header pages, which
// NewOpusReader has already parsed with verification on.
func NewOpusReaderVerifyCRC(r io.Reader, verifyCRC bool) (*OpusReader, error) {
	pr := NewPacketReader(r)
	pr.SetVerifyCRC(verifyCRC)
	pr.SetMaxPacketSize(MaxOpusHeaderPacketSize)
	or := &OpusReader{pr: pr}
	if err := or.readHeaders(); err != nil {
		return nil, err
	}
	pr.SetMaxPacketSize(MaxOpusPacketSize)
	return or, nil
}

// SetMaxPacketSize configures the maximum permitted packet size in bytes.
// A size <= 0 disables the limit.
func (r *OpusReader) SetMaxPacketSize(size int) {
	if r != nil && r.pr != nil {
		r.pr.SetMaxPacketSize(size)
	}
}

// SetVerifyCRC enables or disables CRC checksum verification for read pages.
func (r *OpusReader) SetVerifyCRC(v bool) {
	if r != nil && r.pr != nil {
		r.pr.SetVerifyCRC(v)
	}
}

func (r *OpusReader) readHeaders() error {
	// Opus in Ogg is: BOS page contains OpusHead packet first, then OpusTags.
	pkt1, err := r.pr.ReadPacket()
	if err != nil {
		return err
	}
	if !pkt1.BOS {
		return fmt.Errorf("%w: first packet not BOS", ErrUnexpectedBOS)
	}
	head, err := parseOpusHead(pkt1.Data)
	if err != nil {
		return err
	}
	r.Head = head
	r.headRead = true

	pkt2, err := r.pr.ReadPacket()
	if err != nil {
		return err
	}
	tags, err := parseOpusTags(pkt2.Data)
	if err != nil {
		return err
	}
	r.Tags = tags
	r.tagsRead = true
	return nil
}

// ReadAudioPacket returns the next Opus audio packet (excluding OpusHead/Tags).
// In a chained Ogg Opus bitstream, transitioning to a new logical bitstream will
// automatically update r.Head and r.Tags, and return the first audio packet of the
// new stream with NewStream set to true and StreamIndex incremented.
func (r *OpusReader) ReadAudioPacket() (*OpusAudioPacket, error) {
	if !r.headRead || !r.tagsRead {
		return nil, ErrHeaderSequence
	}
	newStream := false
	for {
		pkt, err := r.pr.ReadPacket()
		if err != nil {
			return nil, err
		}
		if pkt.BOS {
			head, err := parseOpusHead(pkt.Data)
			if err != nil {
				return nil, err
			}
			oldLimit := r.pr.MaxPacketSize
			r.pr.SetMaxPacketSize(MaxOpusHeaderPacketSize)
			tagPkt, err := r.pr.ReadPacket()
			r.pr.SetMaxPacketSize(oldLimit)
			if err != nil {
				return nil, err
			}
			if tagPkt.BOS {
				return nil, fmt.Errorf("%w: second packet in chained stream has BOS", ErrUnexpectedBOS)
			}
			tags, err := parseOpusTags(tagPkt.Data)
			if err != nil {
				return nil, err
			}
			r.Head = head
			r.Tags = tags
			r.streamIndex++
			newStream = true
			continue
		}
		if len(pkt.Data) >= 8 && bytes.Equal(pkt.Data[:8], opusHeadMagic) {
			return nil, ErrBadOpusHead
		}
		if len(pkt.Data) >= 8 && bytes.Equal(pkt.Data[:8], opusTagsMagic) {
			return nil, ErrBadOpusTags
		}
		return &OpusAudioPacket{
			Data:          pkt.Data,
			GranulePos:    pkt.GranulePosition,
			GranuleValid:  pkt.GranuleValid,
			EOS:           pkt.EOS,
			PageSequence:  pkt.PageSequenceEnd,
			Discontinuity: pkt.Discontinuity,
			NewStream:     newStream,
			StreamIndex:   r.streamIndex,
		}, nil
	}
}

// SeekToPage seeks the stream to the page containing or immediately preceding the requested granule position.
// Returns the granule position of the page seeked to.
func (r *OpusReader) SeekToPage(granulePos uint64) (uint64, error) {
	return r.pr.SeekToPage(granulePos)
}

// TotalSamples returns the total number of audio samples in the stream, derived from the final page granule position.
// If the underlying stream is seekable the read position is preserved. Otherwise this reads through to the
// end of the stream, consuming it, and caches the result.
func (r *OpusReader) TotalSamples() (int64, error) {
	// only compute one time
	r.cachedTotalOnce.Do(func() {
		granule, err := r.pr.LastPageGranule()
		if err != nil {
			r.cachedTotalErr = err
		} else {
			samples := granule - int64(r.Head.PreSkip)
			if samples < 0 {
				samples = 0
			}
			r.cachedTotalSamples = samples
			r.cachedTotalErr = nil
		}
	})

	return r.cachedTotalSamples, r.cachedTotalErr
}

// TotalDuration returns the decoded playback duration, derived from TotalSamples.
//
// See TotalSamples for how this affects the read position.
func (r *OpusReader) TotalDuration() (time.Duration, error) {
	samples, err := r.TotalSamples()
	if err != nil {
		return 0, err
	}
	return time.Duration(samples) * time.Second / OpusSampleRateHz, nil
}

func parseOpusHead(b []byte) (OpusHead, error) {
	if len(b) < 19 {
		return OpusHead{}, fmt.Errorf("%w: too short", ErrBadOpusHead)
	}
	if string(b[:8]) != "OpusHead" {
		return OpusHead{}, ErrNotOpusOgg
	}
	h := OpusHead{}
	h.Version = b[8]
	h.Channels = b[9]
	h.PreSkip = binary.LittleEndian.Uint16(b[10:12])
	h.InputSampleRate = binary.LittleEndian.Uint32(b[12:16])
	h.OutputGainQ8 = int16(binary.LittleEndian.Uint16(b[16:18]))
	h.ChannelMappingFamily = b[18]

	if h.Channels == 0 {
		return OpusHead{}, fmt.Errorf("%w: channels=0", ErrBadOpusHead)
	}

	// RFC 7845 Section 5.1: the version is 1, and decoders SHOULD accept 0-15, i.e. any
	// stream whose major version (the upper four bits) is 0.
	if (h.Version >> 4) > 0 {
		return OpusHead{}, fmt.Errorf("%w: unsupported version %d (major version must be 0)", ErrBadOpusHead, h.Version)
	}

	switch h.ChannelMappingFamily {
	case 0:
		if h.Channels > 2 {
			return OpusHead{}, fmt.Errorf("%w: family 0 requires channels <= 2, got %d", ErrBadOpusHead, h.Channels)
		}
	case 1:
		if len(b) < 21 {
			return OpusHead{}, fmt.Errorf("%w: mapping too short", ErrBadOpusHead)
		}
		h.StreamCount = b[19]
		h.CoupledStreamCount = b[20]
		if h.StreamCount == 0 {
			return OpusHead{}, fmt.Errorf("%w: stream count cannot be 0", ErrBadOpusHead)
		}
		if h.CoupledStreamCount > h.StreamCount {
			return OpusHead{}, fmt.Errorf("%w: coupled stream count %d > stream count %d", ErrBadOpusHead, h.CoupledStreamCount, h.StreamCount)
		}
		decoded := int(h.StreamCount) + int(h.CoupledStreamCount)
		if decoded > 255 {
			return OpusHead{}, fmt.Errorf("%w: streams (%d+%d) exceed 255 decoded channels", ErrBadOpusHead, h.StreamCount, h.CoupledStreamCount)
		}
		need := 21 + int(h.Channels)
		if len(b) < need {
			return OpusHead{}, fmt.Errorf("%w: mapping table too short", ErrBadOpusHead)
		}
		h.ChannelMapping = make([]uint8, h.Channels)
		copy(h.ChannelMapping, b[21:need])
		for i, m := range h.ChannelMapping { // each output picks a decoded channel, or 255 for silence
			if int(m) >= decoded && m != 255 {
				return OpusHead{}, fmt.Errorf("%w: channel %d maps to %d but only %d channels are decoded", ErrBadOpusHead, i, m, decoded)
			}
		}
	default:
		return OpusHead{}, fmt.Errorf("%w: unsupported channel mapping family %d", ErrBadOpusHead, h.ChannelMappingFamily)
	}
	return h, nil
}

func parseOpusTags(b []byte) (OpusTags, error) {
	if len(b) < 16 {
		return OpusTags{}, fmt.Errorf("%w: too short", ErrBadOpusTags)
	}
	if string(b[:8]) != "OpusTags" {
		return OpusTags{}, ErrHeaderSequence
	}
	off := 8
	vendorLen := binary.LittleEndian.Uint32(b[off : off+4])
	off += 4
	// Compared as uint64 so a 32-bit int cannot wrap negative and slip past the bound.
	if uint64(vendorLen) > uint64(len(b)-off) {
		return OpusTags{}, fmt.Errorf("%w: bad vendor length", ErrBadOpusTags)
	}
	vendor := string(b[off : off+int(vendorLen)])
	off += int(vendorLen)
	if off+4 > len(b) {
		return OpusTags{}, fmt.Errorf("%w: missing comment count", ErrBadOpusTags)
	}
	rawCount := binary.LittleEndian.Uint32(b[off : off+4])
	off += 4
	maxPossibleComments := (len(b) - off) / 4
	if uint64(rawCount) > uint64(maxPossibleComments) {
		return OpusTags{}, fmt.Errorf("%w: comment count %d exceeds payload capacity", ErrBadOpusTags, rawCount)
	}
	count := int(rawCount)

	comments := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if off+4 > len(b) {
			return OpusTags{}, fmt.Errorf("%w: truncated comment length", ErrBadOpusTags)
		}
		cl := binary.LittleEndian.Uint32(b[off : off+4])
		off += 4
		if uint64(cl) > uint64(len(b)-off) {
			return OpusTags{}, fmt.Errorf("%w: truncated comment", ErrBadOpusTags)
		}
		comments = append(comments, string(b[off:off+int(cl)]))
		off += int(cl)
	}
	return OpusTags{Vendor: vendor, Comments: comments}, nil
}
