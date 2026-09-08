package opus

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	libc "github.com/selawe/go-opus-codec/libcshim"
	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opusccenc"
)

var (
	ErrEncodeFailed = errors.New("opus: encode failed")
	ErrCtlFailed    = errors.New("opus: encoder ctl failed")
)

const (
	ApplicationVoIP               = int(opusccenc.OPUS_APPLICATION_VOIP)
	ApplicationAudio              = int(opusccenc.OPUS_APPLICATION_AUDIO)
	ApplicationRestrictedLowDelay = int(opusccenc.OPUS_APPLICATION_RESTRICTED_LOWDELAY)
	ApplicationRestrictedSilk     = int(opusccenc.OPUS_APPLICATION_RESTRICTED_SILK)
	ApplicationRestrictedCelt     = int(opusccenc.OPUS_APPLICATION_RESTRICTED_CELT)
)

// Encoder is an Opus encoder backed by ccgo-transpiled libopus.
//
// It supports standard mono/stereo encoding as well as multichannel multistream
// encoding (such as 5.1 and 7.1 surround sound).
type Encoder struct {
	mu sync.Mutex

	tls *libc.TLS
	st  uintptr

	sampleRate  int
	channels    int
	application int
	multistream bool

	encBuf []byte
	pcmI16 []int16
	pcmF32 []float32
}

// NewEncoder creates a new pure-Go Opus encoder.
// sampleRate must be 8000, 12000, 16000, 24000, or 48000 Hz. channels must be 1 (mono) or 2 (stereo).
// application is one of ApplicationVoIP, ApplicationAudio, or ApplicationRestrictedLowDelay.
func NewEncoder(sampleRate, channels, application int) (*Encoder, error) {
	tls := libc.NewTLS()
	if tls == nil {
		return nil, errors.New("opus: failed to allocate TLS")
	}

	st, err := opusccenc.Opus_opus_encoder_create(tls, opusccenc.OpusT_opus_int32(sampleRate), int32(channels), int32(application))
	if err != nil || st == 0 {
		if oe := (*opusccenc.OpusError)(nil); errors.As(err, &oe) {
			msg := opusccencErrorString(tls, oe.Code)
			opusccenc.FreePseudostackTLS(tls)
			tls.Close()
			return nil, fmt.Errorf("opus: encoder_create failed: %s (%d)", msg, oe.Code)
		}
		opusccenc.FreePseudostackTLS(tls)
		tls.Close()
		return nil, fmt.Errorf("opus: encoder_create failed: %w", err)
	}

	enc := &Encoder{tls: tls, st: st, sampleRate: sampleRate, channels: channels, application: application}
	runtime.SetFinalizer(enc, (*Encoder).Close)
	return enc, nil
}

// NewMultistreamEncoder creates a new pure-Go Opus multistream encoder for multichannel audio
// (such as 5.1 or 7.1 surround sound) using custom stream and coupled stream mapping tables.
// sampleRate must be 8000, 12000, 16000, 24000, or 48000 Hz.
// channels is the total number of channels (1 to 255).
// streams is the total number of Opus streams to encode (1 to 255).
// coupledStreams is the number of coupled (stereo) streams (0 <= coupledStreams <= streams, and streams + coupledStreams <= channels).
// mapping is an array of size channels mapping each input channel to a stream index.
// application is one of ApplicationVoIP, ApplicationAudio, or ApplicationRestrictedLowDelay.
func NewMultistreamEncoder(sampleRate, channels, streams, coupledStreams int, mapping []uint8, application int) (*Encoder, error) {
	if channels < 1 || channels > 255 {
		return nil, fmt.Errorf("opus: invalid channel count %d (must be 1..255)", channels)
	}
	if streams < 1 || streams > 255 {
		return nil, fmt.Errorf("opus: invalid stream count %d (must be 1..255)", streams)
	}
	if coupledStreams < 0 || coupledStreams > streams {
		return nil, fmt.Errorf("opus: invalid coupled stream count %d (must be 0..%d)", coupledStreams, streams)
	}
	if streams+coupledStreams > channels {
		return nil, fmt.Errorf("opus: streams + coupledStreams (%d) exceeds channels (%d)", streams+coupledStreams, channels)
	}
	if len(mapping) != channels {
		return nil, fmt.Errorf("%w: channel mapping length %d does not match channels %d", ErrUnsupportedMapping, len(mapping), channels)
	}

	tls := libc.NewTLS()
	if tls == nil {
		return nil, errors.New("opus: failed to allocate TLS")
	}

	bp := tls.Alloc(4)
	defer tls.Free(4)
	libc.StoreInt32(bp, 0)

	mappingPtr := libc.PtrUint8(mapping)
	st := opusccenc.Opus_opus_multistream_encoder_create(
		tls,
		opusccenc.OpusT_opus_int32(sampleRate),
		int32(channels),
		int32(streams),
		int32(coupledStreams),
		mappingPtr,
		int32(application),
		uintptr(bp),
	)
	errCode := libc.LoadInt32(bp)
	if st == 0 || errCode != opusccenc.OPUS_OK {
		msg := opusccencErrorString(tls, errCode)
		opusccenc.FreePseudostackTLS(tls)
		tls.Close()
		return nil, fmt.Errorf("opus: multistream_encoder_create failed: %s (%d)", msg, errCode)
	}

	enc := &Encoder{
		tls:         tls,
		st:          st,
		sampleRate:  sampleRate,
		channels:    channels,
		application: application,
		multistream: true,
	}
	runtime.SetFinalizer(enc, (*Encoder).Close)
	return enc, nil
}

// NewEncoderFromHead initializes an Opus encoder matching an Ogg OpusHead header and application mode.
// It supports channel mapping family 0 (mono/stereo) and family 1 (multichannel surround sound).
func NewEncoderFromHead(head ogg.OpusHead, application int) (*Encoder, error) {
	fs := int(head.InputSampleRate)
	if fs == 0 {
		fs = ogg.OpusSampleRateHz
	}

	if head.ChannelMappingFamily == 0 {
		if head.Channels != 1 && head.Channels != 2 {
			return nil, fmt.Errorf("%w: mapping family 0 requires 1 or 2 channels, got %d", ErrUnsupportedMapping, head.Channels)
		}
		return NewEncoder(fs, int(head.Channels), application)
	}

	// Mapping family != 0 uses multistream.
	if head.StreamCount == 0 {
		return nil, fmt.Errorf("%w: missing stream count", ErrUnsupportedMapping)
	}
	if int(head.Channels) != len(head.ChannelMapping) {
		return nil, fmt.Errorf("%w: channel mapping length mismatch", ErrUnsupportedMapping)
	}

	return NewMultistreamEncoder(
		fs,
		int(head.Channels),
		int(head.StreamCount),
		int(head.CoupledStreamCount),
		head.ChannelMapping,
		application,
	)
}

// Close closes the encoder and frees all associated C runtime and TLS memory.
// It is safe to call Close multiple times or concurrently.
func (e *Encoder) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	runtime.SetFinalizer(e, nil)

	if e.tls != nil {
		if e.st != 0 {
			if e.multistream {
				opusccenc.Opus_opus_multistream_encoder_destroy(e.tls, e.st)
			} else {
				opusccenc.Opus_opus_encoder_destroy(e.tls, e.st)
			}
			e.st = 0
		}
		opusccenc.FreePseudostackTLS(e.tls)
		e.tls.Close()
		e.tls = nil
	}
	return nil
}

// SampleRate returns the encoder input sample rate in Hz.
func (e *Encoder) SampleRate() int { return e.sampleRate }

// Channels returns the number of input channels (1 or 2 for basic encoder, up to 255 for multistream).
func (e *Encoder) Channels() int { return e.channels }

// IsMultistream returns true if the encoder was initialized in multistream mode.
func (e *Encoder) IsMultistream() bool { return e.multistream }

// Application returns the configured Opus application mode.
func (e *Encoder) Application() int { return e.application }

// SetBitrate sets the target bitrate in bits per second (e.g. 64000 or 96000).
func (e *Encoder) SetBitrate(bps int) error {
	return e.ctlInt32(int32(opusccenc.OPUS_SET_BITRATE_REQUEST), int32(bps))
}

// SetVBR enables or disables Variable Bitrate (VBR) mode.
func (e *Encoder) SetVBR(enabled bool) error {
	var v int32
	if enabled {
		v = 1
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_VBR_REQUEST), v)
}

// SetComplexity sets the encoder computational complexity (0..10, where 10 gives highest audio quality).
func (e *Encoder) SetComplexity(complexity int) error {
	return e.ctlInt32(int32(opusccenc.OPUS_SET_COMPLEXITY_REQUEST), int32(complexity))
}

// Reset resets the internal encoder state (e.g. between independent audio streams).
func (e *Encoder) Reset() error {
	return e.ctl(int32(opusccenc.OPUS_RESET_STATE))
}

// ResetState resets the internal encoder state. It is an alias for Reset matching OPUS_RESET_STATE.
func (e *Encoder) ResetState() error {
	return e.Reset()
}

// SetDTX configures Discontinuous Transmission (DTX) mode (RFC 6716).
// When enabled, silence or background noise frames are transmitted with minimal bitrate or omitted.
func (e *Encoder) SetDTX(enabled bool) error {
	var v int32
	if enabled {
		v = 1
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_DTX_REQUEST), v)
}

// SetInbandFEC enables or disables in-band Forward Error Correction (FEC).
// When enabled, the encoder adds redundant data into subsequent frames to recover lost packets.
func (e *Encoder) SetInbandFEC(enabled bool) error {
	var v int32
	if enabled {
		v = 1
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_INBAND_FEC_REQUEST), v)
}

// SetPacketLossPerc configures the expected percentage of packet loss in the network (0 to 100).
// Higher values instruct the encoder to dedicate more bitrate to FEC redundancy.
func (e *Encoder) SetPacketLossPerc(percentage int) error {
	if percentage < 0 {
		percentage = 0
	} else if percentage > 100 {
		percentage = 100
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_PACKET_LOSS_PERC_REQUEST), int32(percentage))
}

func (e *Encoder) ctl(request int32) error {
	if e == nil {
		return errors.New("opus: encoder closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.tls == nil || e.st == 0 {
		return errors.New("opus: encoder closed")
	}

	var ret int32
	if e.multistream {
		ret = opusccenc.Opus_opus_multistream_encoder_ctl(e.tls, e.st, request, 0)
	} else {
		ret = opusccenc.Opus_opus_encoder_ctl(e.tls, e.st, request, 0)
	}
	if ret != opusccenc.OPUS_OK {
		return fmt.Errorf("%w: %s (%d)", ErrCtlFailed, opusccencErrorString(e.tls, ret), ret)
	}
	return nil
}

// Lookahead returns the encoder lookahead in samples at 48 kHz.
//
// This is typically used as the OpusHead PreSkip value.
func (e *Encoder) Lookahead() (int, error) {
	if e == nil {
		return 0, errors.New("opus: encoder closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.tls == nil || e.st == 0 {
		return 0, errors.New("opus: encoder closed")
	}

	bp := e.tls.Alloc(32)
	defer e.tls.Free(32)
	// Store the output int32 in the second half to avoid overlap with VaList storage.
	outPtr := bp + 16
	libc.StoreInt32(outPtr, 0)

	var ret int32
	if e.multistream {
		ret = opusccenc.Opus_opus_multistream_encoder_ctl(
			e.tls,
			e.st,
			int32(opusccenc.OPUS_GET_LOOKAHEAD_REQUEST),
			libc.VaList(bp, uintptr(outPtr)),
		)
	} else {
		ret = opusccenc.Opus_opus_encoder_ctl(
			e.tls,
			e.st,
			int32(opusccenc.OPUS_GET_LOOKAHEAD_REQUEST),
			libc.VaList(bp, uintptr(outPtr)),
		)
	}
	if ret != opusccenc.OPUS_OK {
		return 0, fmt.Errorf("%w: %s (%d)", ErrCtlFailed, opusccencErrorString(e.tls, ret), ret)
	}
	return int(libc.LoadInt32(outPtr)), nil
}

// FinalRange returns the final range of the entropy encoder from the most recently encoded frame.
func (e *Encoder) FinalRange() (uint32, error) {
	if e == nil {
		return 0, errors.New("opus: encoder closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.tls == nil || e.st == 0 {
		return 0, errors.New("opus: encoder closed")
	}

	bp := e.tls.Alloc(32)
	defer e.tls.Free(32)

	outPtr := bp + 16
	libc.StoreUint32(outPtr, 0)

	var ret int32
	if e.multistream {
		ret = opusccenc.Opus_opus_multistream_encoder_ctl(
			e.tls,
			e.st,
			int32(opusccenc.OPUS_GET_FINAL_RANGE_REQUEST),
			libc.VaList(bp, uintptr(outPtr)),
		)
	} else {
		ret = opusccenc.Opus_opus_encoder_ctl(
			e.tls,
			e.st,
			int32(opusccenc.OPUS_GET_FINAL_RANGE_REQUEST),
			libc.VaList(bp, uintptr(outPtr)),
		)
	}
	if ret != opusccenc.OPUS_OK {
		return 0, fmt.Errorf("%w: %s (%d)", ErrCtlFailed, opusccencErrorString(e.tls, ret), ret)
	}
	return libc.LoadUint32(outPtr), nil
}

func (e *Encoder) ctlInt32(request int32, value int32) error {
	if e == nil {
		return errors.New("opus: encoder closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.tls == nil || e.st == 0 {
		return errors.New("opus: encoder closed")
	}

	bp := e.tls.Alloc(16)
	defer e.tls.Free(16)

	var ret int32
	if e.multistream {
		ret = opusccenc.Opus_opus_multistream_encoder_ctl(e.tls, e.st, request, libc.VaList(bp, value))
	} else {
		ret = opusccenc.Opus_opus_encoder_ctl(e.tls, e.st, request, libc.VaList(bp, value))
	}
	if ret != opusccenc.OPUS_OK {
		return fmt.Errorf("%w: %s (%d)", ErrCtlFailed, opusccencErrorString(e.tls, ret), ret)
	}
	return nil
}

// Encode encodes interleaved signed 16-bit PCM into a single Opus packet.
//
// frameSize is the number of samples per channel.
func (e *Encoder) Encode(pcm []int16, frameSize int, packet []byte) (int, error) {
	if e == nil {
		return 0, errors.New("opus: encoder closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.tls == nil || e.st == 0 {
		return 0, errors.New("opus: encoder closed")
	}
	if frameSize <= 0 {
		return 0, errors.New("opus: invalid frameSize")
	}
	nNeeded := frameSize * e.channels
	if len(pcm) < nNeeded {
		return 0, fmt.Errorf("opus: pcm buffer too small: need %d samples, have %d", nNeeded, len(pcm))
	}
	if len(packet) == 0 {
		return 0, errors.New("opus: packet buffer is empty")
	}

	// Ensure heap-backed buffers for ccgo interop so pointers remain valid
	// if the goroutine stack grows during transpiled C execution.
	if cap(e.encBuf) < len(packet) {
		e.encBuf = make([]byte, len(packet))
	} else {
		e.encBuf = e.encBuf[:len(packet)]
	}
	if cap(e.pcmI16) < nNeeded {
		e.pcmI16 = make([]int16, nNeeded)
	} else {
		e.pcmI16 = e.pcmI16[:nNeeded]
	}
	copy(e.pcmI16, pcm[:nNeeded])

	pcmPtr := libc.PtrInt16(e.pcmI16)
	outPtr := libc.PtrByte(e.encBuf)

	var ret int32
	if e.multistream {
		ret = opusccenc.Opus_opus_multistream_encode(
			e.tls,
			e.st,
			pcmPtr,
			int32(frameSize),
			outPtr,
			opusccenc.OpusT_opus_int32(len(packet)),
		)
	} else {
		ret = opusccenc.Opus_opus_encode(
			e.tls,
			e.st,
			pcmPtr,
			int32(frameSize),
			outPtr,
			opusccenc.OpusT_opus_int32(len(packet)),
		)
	}
	if ret < 0 {
		return 0, fmt.Errorf("%w: %s (%d)", ErrEncodeFailed, opusccencErrorString(e.tls, int32(ret)), ret)
	}
	copy(packet, e.encBuf[:ret])
	runtime.KeepAlive(e)
	return int(ret), nil
}

// EncodeF32 encodes interleaved float32 PCM (normalized to [-1.0, 1.0]) into a single Opus packet.
//
// frameSize is the number of samples per channel in the input PCM.
// Supported frame sizes at 48 kHz are 120, 240, 480, 960, 1920, and 2880 (2.5, 5, 10, 20, 40, and 60 ms).
//
// Returns the number of bytes written to packet.
func (e *Encoder) EncodeF32(pcm []float32, frameSize int, packet []byte) (int, error) {
	if e == nil {
		return 0, errors.New("opus: encoder closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.tls == nil || e.st == 0 {
		return 0, errors.New("opus: encoder closed")
	}
	if frameSize <= 0 {
		return 0, errors.New("opus: invalid frameSize")
	}
	nNeeded := frameSize * e.channels
	if len(pcm) < nNeeded {
		return 0, fmt.Errorf("opus: pcm buffer too small: need %d samples, have %d", nNeeded, len(pcm))
	}
	if len(packet) == 0 {
		return 0, errors.New("opus: packet buffer is empty")
	}

	// Ensure heap-backed buffers for ccgo interop so pointers remain valid
	// if the goroutine stack grows during transpiled C execution.
	if cap(e.encBuf) < len(packet) {
		e.encBuf = make([]byte, len(packet))
	} else {
		e.encBuf = e.encBuf[:len(packet)]
	}
	if cap(e.pcmF32) < nNeeded {
		e.pcmF32 = make([]float32, nNeeded)
	} else {
		e.pcmF32 = e.pcmF32[:nNeeded]
	}
	copy(e.pcmF32, pcm[:nNeeded])

	pcmPtr := libc.PtrFloat32(e.pcmF32)
	outPtr := libc.PtrByte(e.encBuf)

	var ret int32
	if e.multistream {
		ret = opusccenc.Opus_opus_multistream_encode_float(
			e.tls,
			e.st,
			pcmPtr,
			int32(frameSize),
			outPtr,
			opusccenc.OpusT_opus_int32(len(packet)),
		)
	} else {
		ret = opusccenc.Opus_opus_encode_float(
			e.tls,
			e.st,
			pcmPtr,
			int32(frameSize),
			outPtr,
			opusccenc.OpusT_opus_int32(len(packet)),
		)
	}
	if ret < 0 {
		return 0, fmt.Errorf("%w: %s (%d)", ErrEncodeFailed, opusccencErrorString(e.tls, int32(ret)), ret)
	}
	copy(packet, e.encBuf[:ret])
	runtime.KeepAlive(e)
	return int(ret), nil
}

func opusccencErrorString(tls *libc.TLS, code int32) string {
	_ = tls
	s := opusccenc.Opus_opus_strerror(code)
	if s == "" {
		return "(unknown)"
	}
	return s
}
