// Package opus provides a pure-Go implementation of the Opus Interactive Audio Codec
// (RFC 6716 and RFC 8251 errata), backed by transpiled libopus.
//
// The package supports high-performance encoding and decoding at sample rates up to
// 48000 Hz, with support for mono, stereo, and multichannel surround (5.1 / 7.1) configurations.
// Both 16-bit linear PCM and 32-bit floating-point PCM formats are supported.
//
// Key features:
//   - Pure Go: 100% Go with no external C dependencies or cgo required.
//   - Packet inspection: ParsePacketTOC, PacketFrames, PacketHasLBRR, PacketDuration,
//     PacketChannels, PacketBandwidth, and PacketTotalSamples.
//   - Safe Packet Loss Concealment (PLC): Decoding nil packets synthesizes missing audio.
//   - Codec controls: Reset, SetGain, SetInBandFEC, SetDTX, SetPacketLossPerc, and SetBitrate.
//   - Thread safety: All Decoder and Encoder methods are thread-safe and protected by mutexes.
package opus

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	libc "github.com/selawe/go-opus-codec/libcshim"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opuscc"
)

var (
	ErrUnsupportedMapping = errors.New("opus: unsupported channel mapping")
	ErrBadPacket          = errors.New("opus: decode failed")
)

// Decoder is a pure-Go Opus decoder backed by ccgo-transpiled libopus.
//
// It supports both mapping family 0 (mono/stereo) and multistream mappings (surround 5.1 / 7.1).
// All public methods on Decoder are thread-safe and protected by a mutex.
type Decoder struct {
	mu sync.Mutex

	tls *libc.TLS
	st  uintptr

	sampleRate int
	channels   int

	multistream bool

	pcmI16 []int16
	pcmF32 []float32
}

// NewDecoderFromHead initializes an Opus decoder configured according to an Ogg OpusHead header.
// It supports channel mapping family 0 (mono/stereo) and family 1 (multichannel surround),
// and automatically applies the header's OutputGainQ8 per RFC 7845 Section 5.1.1.
func NewDecoderFromHead(head ogg.OpusHead) (*Decoder, error) {
	// RFC 7845: Opus is decoded at 48 kHz.
	const fs = ogg.OpusSampleRateHz

	var dec *Decoder
	var err error
	if head.ChannelMappingFamily == 0 {
		if head.Channels != 1 && head.Channels != 2 {
			return nil, fmt.Errorf("%w: mapping family 0 requires 1 or 2 channels, got %d", ErrUnsupportedMapping, head.Channels)
		}
		dec, err = NewDecoder(fs, int(head.Channels))
	} else {
		// Mapping family != 0 uses multistream.
		if head.StreamCount == 0 {
			return nil, fmt.Errorf("%w: missing stream count", ErrUnsupportedMapping)
		}
		if int(head.Channels) != len(head.ChannelMapping) {
			return nil, fmt.Errorf("%w: channel mapping length mismatch", ErrUnsupportedMapping)
		}

		dec, err = NewMultistreamDecoder(fs, int(head.Channels), int(head.StreamCount), int(head.CoupledStreamCount), head.ChannelMapping)
	}
	if err != nil {
		return nil, err
	}

	// RFC 7845 Section 5.1.1: Output gain in Q7.8 dB units must be applied by players/decoders.
	if head.OutputGainQ8 != 0 {
		if err := dec.SetGain(int(head.OutputGainQ8)); err != nil {
			_ = dec.Close()
			return nil, fmt.Errorf("opus: apply output gain: %w", err)
		}
	}

	return dec, nil
}

// NewDecoder creates a new Opus decoder for standard mono or stereo audio.
// sampleRate is typically 48000, and channels must be 1 or 2.
func NewDecoder(sampleRate, channels int) (*Decoder, error) {
	tls := libc.NewTLS()
	if tls == nil {
		return nil, errors.New("opus: failed to allocate TLS")
	}

	st, err := opuscc.Opus_opus_decoder_create(tls, opuscc.OpusT_opus_int32(sampleRate), int32(channels))
	if err != nil || st == 0 {
		if oe := (*opuscc.OpusError)(nil); errors.As(err, &oe) {
			msg := opusccErrorString(oe.Code)
			tls.Close()
			return nil, fmt.Errorf("opus: decoder_create failed: %s (%d)", msg, oe.Code)
		}
		tls.Close()
		return nil, fmt.Errorf("opus: decoder_create failed: %w", err)
	}

	dec := &Decoder{tls: tls, st: st, sampleRate: sampleRate, channels: channels}
	runtime.SetFinalizer(dec, (*Decoder).Close)
	return dec, nil
}

// NewMultistreamDecoder creates an Opus multistream decoder for multichannel surround audio
// (such as 5.1 or 7.1 surround sound) using custom stream and coupled stream mapping tables.
func NewMultistreamDecoder(sampleRate, channels, streams, coupledStreams int, mapping []uint8) (*Decoder, error) {
	tls := libc.NewTLS()
	if tls == nil {
		return nil, errors.New("opus: failed to allocate TLS")
	}

	mappingPtr := libc.PtrUint8(mapping)

	st, err := opuscc.Opus_opus_multistream_decoder_create(
		tls,
		opuscc.OpusT_opus_int32(sampleRate),
		int32(channels),
		int32(streams),
		int32(coupledStreams),
		mappingPtr,
	)
	if err != nil || st == 0 {
		if oe := (*opuscc.OpusError)(nil); errors.As(err, &oe) {
			msg := opusccErrorString(oe.Code)
			tls.Close()
			return nil, fmt.Errorf("opus: multistream_decoder_create failed: %s (%d)", msg, oe.Code)
		}
		tls.Close()
		return nil, fmt.Errorf("opus: multistream_decoder_create failed: %w", err)
	}

	dec := &Decoder{tls: tls, st: st, sampleRate: sampleRate, channels: channels, multistream: true}
	runtime.SetFinalizer(dec, (*Decoder).Close)
	return dec, nil
}

// Close closes the decoder and frees all associated C runtime and TLS memory.
// It is safe to call Close multiple times or concurrently.
func (d *Decoder) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	runtime.SetFinalizer(d, nil)

	if d.tls != nil {
		if d.st != 0 {
			if d.multistream {
				opuscc.Opus_opus_multistream_decoder_destroy(d.tls, d.st)
			} else {
				opuscc.Opus_opus_decoder_destroy(d.tls, d.st)
			}
			d.st = 0
		}
		opuscc.FreePseudostackTLS(d.tls)
		d.tls.Close()
		d.tls = nil
	}
	return nil
}

// SampleRate returns the sample rate in Hz (always 48000 for Opus).
func (d *Decoder) SampleRate() int { return d.sampleRate }

// Channels returns the number of channels decoded (1 for mono, 2 for stereo, up to 8 for surround).
func (d *Decoder) Channels() int   { return d.channels }

// Decode decodes a single Opus packet into interleaved signed 16-bit PCM.
//
// frameSize is the maximum number of samples per channel to decode.
// Use 5760 for the Opus max frame size at 48kHz (120 ms).
//
// Returns the number of samples per channel written into pcm.
func (d *Decoder) Decode(packet []byte, pcm []int16, frameSize int, decodeFEC bool) (int, error) {
	if d == nil {
		return 0, errors.New("opus: decoder closed")
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d == nil || d.tls == nil || d.st == 0 {
		return 0, errors.New("opus: decoder closed")
	}
	if frameSize <= 0 {
		return 0, errors.New("opus: invalid frameSize")
	}
	nNeeded := frameSize * d.channels
	if len(pcm) < nNeeded {
		return 0, fmt.Errorf("opus: pcm buffer too small: need %d samples, have %d", nNeeded, len(pcm))
	}

	// Use heap-backed staging buffer so pointers passed to ccgo remain valid
	// even if the caller's slice was stack-allocated and moved during stack growth.
	if cap(d.pcmI16) < nNeeded {
		d.pcmI16 = make([]int16, nNeeded)
	} else {
		d.pcmI16 = d.pcmI16[:nNeeded]
	}

	dataPtr := libc.PtrByte(packet)
	dataLen := int32(len(packet))
	pcmPtr := libc.PtrInt16(d.pcmI16)
	fec := int32(0)
	if decodeFEC {
		fec = 1
	}

	var ret int32
	if d.multistream {
		ret = opuscc.Opus_opus_multistream_decode(d.tls, d.st, dataPtr, opuscc.OpusT_opus_int32(dataLen), pcmPtr, int32(frameSize), fec)
	} else {
		ret = opuscc.Opus_opus_decode(d.tls, d.st, dataPtr, opuscc.OpusT_opus_int32(dataLen), pcmPtr, int32(frameSize), fec)
	}

	if ret < 0 {
		return 0, fmt.Errorf("%w: %s (%d)", ErrBadPacket, opusccErrorString(ret), ret)
	}
	nDecoded := int(ret) * d.channels
	copy(pcm[:nDecoded], d.pcmI16[:nDecoded])
	runtime.KeepAlive(d)
	return int(ret), nil
}

// DecodeF32 decodes a single Opus packet into interleaved 32-bit float PCM.
//
// frameSize is the maximum number of samples per channel to decode.
// Use 5760 for the Opus max frame size at 48kHz (120 ms).
//
// Returns the number of samples per channel written into pcm.
func (d *Decoder) DecodeF32(packet []byte, pcm []float32, frameSize int, decodeFEC bool) (int, error) {
	if d == nil {
		return 0, errors.New("opus: decoder closed")
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d == nil || d.tls == nil || d.st == 0 {
		return 0, errors.New("opus: decoder closed")
	}
	if frameSize <= 0 {
		return 0, errors.New("opus: invalid frameSize")
	}
	nNeeded := frameSize * d.channels
	if len(pcm) < nNeeded {
		return 0, fmt.Errorf("opus: pcm buffer too small: need %d samples, have %d", nNeeded, len(pcm))
	}

	// Use heap-backed staging buffer so pointers passed to ccgo remain valid
	// even if the caller's slice was stack-allocated and moved during stack growth.
	if cap(d.pcmF32) < nNeeded {
		d.pcmF32 = make([]float32, nNeeded)
	} else {
		d.pcmF32 = d.pcmF32[:nNeeded]
	}

	dataPtr := libc.PtrByte(packet)
	dataLen := int32(len(packet))
	pcmPtr := libc.PtrFloat32(d.pcmF32)
	fec := int32(0)
	if decodeFEC {
		fec = 1
	}

	var ret int32
	if d.multistream {
		ret = opuscc.Opus_opus_multistream_decode_float(d.tls, d.st, dataPtr, opuscc.OpusT_opus_int32(dataLen), pcmPtr, int32(frameSize), fec)
	} else {
		ret = opuscc.Opus_opus_decode_float(d.tls, d.st, dataPtr, opuscc.OpusT_opus_int32(dataLen), pcmPtr, int32(frameSize), fec)
	}

	if ret < 0 {
		return 0, fmt.Errorf("%w: %s (%d)", ErrBadPacket, opusccErrorString(ret), ret)
	}
	nDecoded := int(ret) * d.channels
	copy(pcm[:nDecoded], d.pcmF32[:nDecoded])
	runtime.KeepAlive(d)
	return int(ret), nil
}

// Reset resets the internal decoder state (e.g. after seeking or stream discontinuity).
func (d *Decoder) Reset() error {
	return d.ctl(int32(opuscc.OPUS_RESET_STATE))
}

// ResetState resets the internal decoder state. It is an alias for Reset matching OPUS_RESET_STATE.
func (d *Decoder) ResetState() error {
	return d.Reset()
}

// SetGain sets the decoder output gain in Q7.8 dB units (RFC 7845 Section 5.1.1).
// For example, 0 is 0 dB, 256 is +1 dB, -256 is -1 dB.
func (d *Decoder) SetGain(gainQ8 int) error {
	return d.ctlInt32(int32(opuscc.OPUS_SET_GAIN_REQUEST), int32(gainQ8))
}

func (d *Decoder) ctl(request int32) error {
	if d == nil {
		return errors.New("opus: decoder closed")
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.tls == nil || d.st == 0 {
		return errors.New("opus: decoder closed")
	}

	var ret int32
	if d.multistream {
		ret = opuscc.Opus_opus_multistream_decoder_ctl(d.tls, d.st, request, 0)
	} else {
		ret = opuscc.Opus_opus_decoder_ctl(d.tls, d.st, request, 0)
	}
	if ret != opuscc.OPUS_OK {
		return fmt.Errorf("opus: decoder ctl failed: %s (%d)", opusccErrorString(ret), ret)
	}
	return nil
}

func (d *Decoder) ctlInt32(request int32, value int32) error {
	if d == nil {
		return errors.New("opus: decoder closed")
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.tls == nil || d.st == 0 {
		return errors.New("opus: decoder closed")
	}

	bp := d.tls.Alloc(16)
	defer d.tls.Free(16)

	var ret int32
	if d.multistream {
		ret = opuscc.Opus_opus_multistream_decoder_ctl(d.tls, d.st, request, libc.VaList(bp, value))
	} else {
		ret = opuscc.Opus_opus_decoder_ctl(d.tls, d.st, request, libc.VaList(bp, value))
	}
	if ret != opuscc.OPUS_OK {
		return fmt.Errorf("opus: decoder ctl failed: %s (%d)", opusccErrorString(ret), ret)
	}
	return nil
}

// FinalRange returns the final range of the entropy decoder from the most recently decoded frame.
// In RFC 6716 Section 6, this value is compared against the expected final range to verify bit-exact
// entropy decoding conformance.
func (d *Decoder) FinalRange() (uint32, error) {
	if d == nil {
		return 0, errors.New("opus: decoder closed")
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.tls == nil || d.st == 0 {
		return 0, errors.New("opus: decoder closed")
	}

	bp := d.tls.Alloc(32)
	defer d.tls.Free(32)

	outPtr := bp + 16
	libc.StoreUint32(outPtr, 0)

	var ret int32
	if d.multistream {
		ret = opuscc.Opus_opus_multistream_decoder_ctl(d.tls, d.st, int32(opuscc.OPUS_GET_FINAL_RANGE_REQUEST), libc.VaList(bp, uintptr(outPtr)))
	} else {
		ret = opuscc.Opus_opus_decoder_ctl(d.tls, d.st, int32(opuscc.OPUS_GET_FINAL_RANGE_REQUEST), libc.VaList(bp, uintptr(outPtr)))
	}
	if ret != opuscc.OPUS_OK {
		return 0, fmt.Errorf("opus: get final range failed: %s (%d)", opusccErrorString(ret), ret)
	}
	return libc.LoadUint32(outPtr), nil
}

// DecodePacket decodes an Ogg OpusAudioPacket into interleaved signed 16-bit PCM.
// If pcm is nil or smaller than the maximum packet size (120ms at 48kHz), a new buffer is allocated.
// If packet is nil or packet.Data is nil, Packet Loss Concealment (PLC) is safely triggered per RFC 6716 Section 3.4.
// Returns the decoded sub-slice of pcm and the number of samples per channel.
func (decoder *Decoder) DecodePacket(packet *ogg.OpusAudioPacket, pcm []int16) ([]int16, int, error) {
	const maxMsPerFrame = 120
	maxSize := ogg.OpusSampleRateHz * maxMsPerFrame / 1000
	if len(pcm) < maxSize*decoder.channels {
		pcm = make([]int16, maxSize*decoder.channels)
	}

	var data []byte
	if packet != nil {
		data = packet.Data
	}

	n, err := decoder.Decode(data, pcm, maxSize, false)
	if err != nil {
		return nil, 0, err
	}

	return pcm[:n*decoder.channels], n, nil
}

// DecodePacketF32 decodes an Ogg OpusAudioPacket into interleaved 32-bit float PCM.
// If pcm is nil or smaller than the maximum packet size (120ms at 48kHz), a new buffer is allocated.
// If packet is nil or packet.Data is nil, Packet Loss Concealment (PLC) is safely triggered per RFC 6716 Section 3.4.
// Returns the decoded sub-slice of pcm and the number of samples per channel.
func (decoder *Decoder) DecodePacketF32(packet *ogg.OpusAudioPacket, pcm []float32) ([]float32, int, error) {
	const maxMsPerFrame = 120
	maxSize := ogg.OpusSampleRateHz * maxMsPerFrame / 1000
	if len(pcm) < maxSize*decoder.channels {
		pcm = make([]float32, maxSize*decoder.channels)
	}

	var data []byte
	if packet != nil {
		data = packet.Data
	}

	n, err := decoder.DecodeF32(data, pcm, maxSize, false)
	if err != nil {
		return nil, 0, err
	}

	return pcm[:n*decoder.channels], n, nil
}

func opusccErrorString(code int32) string {
	s := opuscc.Opus_opus_strerror(code)
	if s == "" {
		return "(unknown)"
	}
	return s
}
