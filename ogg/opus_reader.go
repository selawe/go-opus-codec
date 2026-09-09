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
}

// OpusReader reads an Ogg Opus file/stream and yields Opus audio packets.
type OpusReader struct {
	pr *PacketReader

	Head OpusHead
	Tags OpusTags

	headRead bool
	tagsRead bool

	cachedTotalSamples int64
	cachedTotalErr     error
	cachedTotalOnce    sync.Once
}

// OpusSampleRateHz is the Opus decoding sample rate (RFC 7845).
const OpusSampleRateHz = 48000

// MaxOpusPacketSize is the maximum permitted size (64 KiB) for an Opus audio packet in Ogg Opus (RFC 7845 / RFC 6716).
// A single Opus packet can contain up to 48 frames with maximum 120 ms audio (~61,200 bytes) plus header/padding.
const MaxOpusPacketSize = 64 * 1024

// NewOpusReader creates a new OpusReader reading from r, parsing the mandatory OpusHead and OpusTags headers.
func NewOpusReader(r io.Reader) (*OpusReader, error) {
	pr := NewPacketReader(r)
	pr.SetMaxPacketSize(MaxOpusPacketSize)
	or := &OpusReader{pr: pr}
	if err := or.readHeaders(); err != nil {
		return nil, err
	}
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
func (r *OpusReader) ReadAudioPacket() (*OpusAudioPacket, error) {
	if !r.headRead || !r.tagsRead {
		return nil, ErrHeaderSequence
	}
	pkt, err := r.pr.ReadPacket()
	if err != nil {
		return nil, err
	}
	if pkt.BOS {
		return nil, fmt.Errorf("%w: BOS after headers", ErrUnexpectedBOS)
	}
	if len(pkt.Data) >= 8 && bytes.Equal(pkt.Data[:8], opusHeadMagic) {
		return nil, ErrBadOpusHead
	}
	if len(pkt.Data) >= 8 && bytes.Equal(pkt.Data[:8], opusTagsMagic) {
		return nil, ErrBadOpusTags
	}
	return &OpusAudioPacket{
		Data:         pkt.Data,
		GranulePos:   pkt.GranulePosition,
		GranuleValid: pkt.GranuleValid,
		EOS:          pkt.EOS,
		PageSequence: pkt.PageSequenceEnd,
	}, nil
}

// SeekToPage seeks the stream to the page containing or immediately preceding the requested granule position.
// Returns the granule position of the page seeked to.
func (r *OpusReader) SeekToPage(granulePos uint64) (uint64, error) {
	return r.pr.SeekToPage(granulePos)
}

// TotalSamples returns the total number of audio samples in the stream, derived from the final page granule position.
// Note: If the underlying stream is not seekable, this reads through to the end and caches the result.
func (r *OpusReader) TotalSamples() (int64, error) {
	// only compute one time
	r.cachedTotalOnce.Do(func() {
		granule, err := r.pr.LastPageGranule()
		if err != nil {
			r.cachedTotalErr = err
		} else {
			r.cachedTotalSamples = granule - int64(r.Head.PreSkip)
			r.cachedTotalErr = nil
		}
	})

	return r.cachedTotalSamples, r.cachedTotalErr
}

// TotalDuration returns the decoded playback duration, derived from TotalSamples.
//
// Note: This method consumes packets until EOF, see the note about TotalSamples.
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

	// RFC 7845 Section 5.1: version number must have major version 0 (values 1..15, with 1 being standard).
	if h.Version == 0 || (h.Version>>4) > 0 {
		return OpusHead{}, fmt.Errorf("%w: unsupported version %d (major version must be 0)", ErrBadOpusHead, h.Version)
	}

	if h.ChannelMappingFamily == 0 {
		if h.Channels > 2 {
			return OpusHead{}, fmt.Errorf("%w: family 0 requires channels <= 2, got %d", ErrBadOpusHead, h.Channels)
		}
	} else if h.ChannelMappingFamily == 1 {
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
		if int(h.StreamCount)+int(h.CoupledStreamCount) > int(h.Channels) {
			return OpusHead{}, fmt.Errorf("%w: streams (%d+%d) > channels (%d)", ErrBadOpusHead, h.StreamCount, h.CoupledStreamCount, h.Channels)
		}
		need := 21 + int(h.Channels)
		if len(b) < need {
			return OpusHead{}, fmt.Errorf("%w: mapping table too short", ErrBadOpusHead)
		}
		h.ChannelMapping = make([]uint8, h.Channels)
		copy(h.ChannelMapping, b[21:need])
	} else {
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
	vendorLen := int(binary.LittleEndian.Uint32(b[off : off+4]))
	off += 4
	if vendorLen < 0 || off+vendorLen > len(b) {
		return OpusTags{}, fmt.Errorf("%w: bad vendor length", ErrBadOpusTags)
	}
	vendor := string(b[off : off+vendorLen])
	off += vendorLen
	if off+4 > len(b) {
		return OpusTags{}, fmt.Errorf("%w: missing comment count", ErrBadOpusTags)
	}
	count := int(binary.LittleEndian.Uint32(b[off : off+4]))
	off += 4
	maxPossibleComments := (len(b) - off) / 4
	if count > maxPossibleComments {
		return OpusTags{}, fmt.Errorf("%w: comment count %d exceeds payload capacity", ErrBadOpusTags, count)
	}

	comments := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if off+4 > len(b) {
			return OpusTags{}, fmt.Errorf("%w: truncated comment length", ErrBadOpusTags)
		}
		cl := int(binary.LittleEndian.Uint32(b[off : off+4]))
		off += 4
		if cl < 0 || off+cl > len(b) {
			return OpusTags{}, fmt.Errorf("%w: truncated comment", ErrBadOpusTags)
		}
		comments = append(comments, string(b[off:off+cl]))
		off += cl
	}
	return OpusTags{Vendor: vendor, Comments: comments}, nil
}
