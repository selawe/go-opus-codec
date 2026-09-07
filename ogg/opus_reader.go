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

type OpusAudioPacket struct {
	Data         []byte
	GranulePos   uint64
	GranuleValid bool
	EOS          bool
	PageSequence uint32
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

func NewOpusReader(r io.Reader) (*OpusReader, error) {
	or := &OpusReader{pr: NewPacketReader(r)}
	if err := or.readHeaders(); err != nil {
		return nil, err
	}
	return or, nil
}

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

// returns the granule position of the sequence just before the requested granule position
func (r *OpusReader) SeekToPage(granulePos uint64) (uint64, error) {
	return r.pr.SeekToPage(granulePos)
}

// return the total number of samples in the stream, derived from the last page granule position.
// Note: this method is destructive in that it reads packets. Subsequent calls to ReadAudioPacket may return EOF.
// if the underlying reader is seekable, you may want to seek back to the start after calling this method.
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

	if h.ChannelMappingFamily == 0 {
		if h.Channels > 2 {
			return OpusHead{}, fmt.Errorf("%w: family 0 requires channels <= 2, got %d", ErrBadOpusHead, h.Channels)
		}
	} else {
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
