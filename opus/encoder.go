package opus

import (
	"errors"
	"fmt"
	"math"
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

// Standard Opus applications (RFC 6716 Section 5.1).
const (
	// ApplicationVoIP optimizes for speech clarity, packet loss resilience, and lower bitrates.
	ApplicationVoIP = int(opusccenc.OPUS_APPLICATION_VOIP)

	// ApplicationAudio optimizes for high-fidelity music and general broadcast audio.
	ApplicationAudio = int(opusccenc.OPUS_APPLICATION_AUDIO)

	// ApplicationRestrictedLowDelay operates exclusively with low algorithmic delay modes.
	ApplicationRestrictedLowDelay = int(opusccenc.OPUS_APPLICATION_RESTRICTED_LOWDELAY)

	// ApplicationRestrictedSilk is an internal libopus mode used for compliance and certification
	// testing (forces SILK-only mode). It is not intended for general production use.
	ApplicationRestrictedSilk = int(opusccenc.OPUS_APPLICATION_RESTRICTED_SILK)

	// ApplicationRestrictedCelt is an internal libopus mode used for compliance and certification
	// testing (forces CELT-only mode). It is not intended for general production use.
	ApplicationRestrictedCelt = int(opusccenc.OPUS_APPLICATION_RESTRICTED_CELT)
)

// Auto is the automatic parameter selection value used by some Opus controls
// (e.g. BandwidthAuto, SignalAuto, and automatic channel count in SetForceChannels).
const Auto = -1000

// Signal specifies the audio signal type hint for the encoder.
type Signal int

const (
	// SignalAuto lets the encoder detect voice vs music automatically.
	SignalAuto Signal = Auto

	// SignalVoice biases mode decisions towards speech clarity.
	SignalVoice Signal = Signal(opusccenc.OPUS_SIGNAL_VOICE)

	// SignalMusic biases mode decisions towards music fidelity.
	SignalMusic Signal = Signal(opusccenc.OPUS_SIGNAL_MUSIC)
)

func (s Signal) String() string {
	switch s {
	case SignalAuto:
		return "auto"
	case SignalVoice:
		return "voice"
	case SignalMusic:
		return "music"
	default:
		return fmt.Sprintf("Signal(%d)", int(s))
	}
}

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
	libc.StoreInt32(bp, 0)

	// Heap copy: the caller's table may live on a goroutine stack that can move.
	mappingCopy := append([]uint8(nil), mapping...)
	mappingPtr := libc.PtrUint8(mappingCopy)
	st := opusccenc.Opus_opus_multistream_encoder_create(
		tls,
		opusccenc.OpusT_opus_int32(sampleRate),
		int32(channels),
		int32(streams),
		int32(coupledStreams),
		mappingPtr,
		int32(application),
		bp,
	)
	runtime.KeepAlive(mappingCopy)
	errCode := libc.LoadInt32(bp)
	// Free before any tls.Close(): Close resets the TLS stack pointer, so a
	// deferred Free would underflow on the error path.
	tls.Free(4)
	if st == 0 || errCode != opusccenc.OPUS_OK {
		if errCode == opusccenc.OPUS_OK {
			errCode = opusAllocFail // a nil encoder without an error code is an allocation failure
		}
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
	if fs != 8000 && fs != 12000 && fs != 16000 && fs != 24000 && fs != 48000 {
		fs = ogg.OpusSampleRateHz
	}

	switch head.ChannelMappingFamily {
	case 0:
		if head.Channels != 1 && head.Channels != 2 {
			return nil, fmt.Errorf("%w: mapping family 0 requires 1 or 2 channels, got %d", ErrUnsupportedMapping, head.Channels)
		}
		return NewEncoder(fs, int(head.Channels), application)
	case 1:
		// Mapping family 1 uses multistream.
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
	default:
		return nil, fmt.Errorf("%w: unsupported channel mapping family %d", ErrUnsupportedMapping, head.ChannelMappingFamily)
	}
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
	v, err := int32Arg("bitrate", bps)
	if err != nil {
		return err
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_BITRATE_REQUEST), v)
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
	v, err := int32Arg("complexity", complexity)
	if err != nil {
		return err
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_COMPLEXITY_REQUEST), v)
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
	if percentage < 0 || percentage > 100 {
		return fmt.Errorf("opus: invalid packet loss percentage %d (must be 0..100)", percentage)
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

// Bitrate returns the current configured target bitrate in bits per second.
func (e *Encoder) Bitrate() (int, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_BITRATE_REQUEST))
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

// VBR returns whether Variable Bitrate (VBR) mode is enabled.
func (e *Encoder) VBR() (bool, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_VBR_REQUEST))
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// SetVBRConstraint enables or disables Constrained VBR mode.
// When enabled, bitrate fluctuations are bounded within a target constraint window.
func (e *Encoder) SetVBRConstraint(constrained bool) error {
	var v int32
	if constrained {
		v = 1
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_VBR_CONSTRAINT_REQUEST), v)
}

// VBRConstraint returns whether Constrained VBR mode is enabled.
func (e *Encoder) VBRConstraint() (bool, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_VBR_CONSTRAINT_REQUEST))
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// Complexity returns the current encoder computational complexity (0..10).
func (e *Encoder) Complexity() (int, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_COMPLEXITY_REQUEST))
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

// DTX returns whether Discontinuous Transmission (DTX) mode is enabled.
func (e *Encoder) DTX() (bool, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_DTX_REQUEST))
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// InbandFEC returns whether in-band Forward Error Correction (FEC) is enabled.
func (e *Encoder) InbandFEC() (bool, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_INBAND_FEC_REQUEST))
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// PacketLossPerc returns the configured expected percentage of packet loss (0..100).
func (e *Encoder) PacketLossPerc() (int, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_PACKET_LOSS_PERC_REQUEST))
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

// SetBandwidth sets the audio bandwidth of the encoder.
func (e *Encoder) SetBandwidth(bw Bandwidth) error {
	switch bw {
	case BandwidthAuto, BandwidthNarrowband, BandwidthMediumband, BandwidthWideband, BandwidthSuperwideband, BandwidthFullband:
	default:
		return fmt.Errorf("opus: invalid bandwidth %d", bw)
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_BANDWIDTH_REQUEST), int32(bw))
}

// Bandwidth returns the current audio bandwidth configured on the encoder.
func (e *Encoder) Bandwidth() (Bandwidth, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_BANDWIDTH_REQUEST))
	if err != nil {
		return 0, err
	}
	return Bandwidth(v), nil
}

// SetMaxBandwidth configures the maximum audio bandwidth the encoder may use.
// Unlike SetBandwidth, BandwidthAuto is not a valid value for SetMaxBandwidth.
func (e *Encoder) SetMaxBandwidth(bw Bandwidth) error {
	switch bw {
	case BandwidthNarrowband, BandwidthMediumband, BandwidthWideband, BandwidthSuperwideband, BandwidthFullband:
	default:
		return fmt.Errorf("opus: invalid max bandwidth %d (must be between BandwidthNarrowband and BandwidthFullband)", bw)
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_MAX_BANDWIDTH_REQUEST), int32(bw))
}

// MaxBandwidth returns the maximum audio bandwidth configured on the encoder.
func (e *Encoder) MaxBandwidth() (Bandwidth, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_MAX_BANDWIDTH_REQUEST))
	if err != nil {
		return 0, err
	}
	return Bandwidth(v), nil
}

// SetSignal configures the audio signal type hint.
func (e *Encoder) SetSignal(signal Signal) error {
	switch signal {
	case SignalAuto, SignalVoice, SignalMusic:
	default:
		return fmt.Errorf("opus: invalid signal %d", signal)
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_SIGNAL_REQUEST), int32(signal))
}

// Signal returns the audio signal type hint configured on the encoder.
func (e *Encoder) Signal() (Signal, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_SIGNAL_REQUEST))
	if err != nil {
		return 0, err
	}
	return Signal(v), nil
}

// SetForceChannels forces mono or stereo encoding, or enables automatic channel decision.
// Allowed values are Auto (-1000), 1 (mono), or 2 (stereo, only on stereo encoders).
func (e *Encoder) SetForceChannels(channels int) error {
	if channels != Auto && channels != 1 && channels != 2 {
		return fmt.Errorf("opus: invalid forced channels %d (must be Auto, 1, or 2)", channels)
	}
	if channels > e.channels {
		return fmt.Errorf("opus: cannot force %d channels on a %d-channel encoder", channels, e.channels)
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_FORCE_CHANNELS_REQUEST), int32(channels))
}

// ForceChannels returns the forced channels setting.
func (e *Encoder) ForceChannels() (int, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_FORCE_CHANNELS_REQUEST))
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

// SetLSBDepth sets the resolution hint of the input audio in bits (8..24).
// This guides quantization noise shaping in the encoder.
func (e *Encoder) SetLSBDepth(depth int) error {
	if depth < 8 || depth > 24 {
		return fmt.Errorf("opus: invalid LSB depth %d (must be 8..24)", depth)
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_LSB_DEPTH_REQUEST), int32(depth))
}

// LSBDepth returns the input resolution hint in bits.
func (e *Encoder) LSBDepth() (int, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_LSB_DEPTH_REQUEST))
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

// SetPredictionDisabled disables almost all inter-frame prediction when true,
// making frames more independent at the cost of bitrate efficiency.
func (e *Encoder) SetPredictionDisabled(disabled bool) error {
	var v int32
	if disabled {
		v = 1
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_PREDICTION_DISABLED_REQUEST), v)
}

// PredictionDisabled returns whether inter-frame prediction is disabled.
func (e *Encoder) PredictionDisabled() (bool, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_PREDICTION_DISABLED_REQUEST))
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// SetPhaseInversionDisabled disables phase inversion (stereo decorrelation) when true.
func (e *Encoder) SetPhaseInversionDisabled(disabled bool) error {
	var v int32
	if disabled {
		v = 1
	}
	return e.ctlInt32(int32(opusccenc.OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST), v)
}

// PhaseInversionDisabled returns whether phase inversion is disabled.
func (e *Encoder) PhaseInversionDisabled() (bool, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST))
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// Lookahead returns the encoder lookahead in samples at the encoder's sample rate.
//
// When writing an Ogg OpusHead header, RFC 7845 Section 5.1 requires PreSkip to be
// represented in 48 kHz samples: lookahead * 48000 / sampleRate.
func (e *Encoder) Lookahead() (int, error) {
	v, err := e.ctlGetInt32(int32(opusccenc.OPUS_GET_LOOKAHEAD_REQUEST))
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

func (e *Encoder) ctlGetInt32(request int32) (int32, error) {
	if e == nil {
		return 0, errors.New("opus: encoder closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.tls == nil || e.st == 0 {
		return 0, errors.New("opus: encoder closed")
	}

	bp := e.tls.Alloc(64)
	defer e.tls.Free(64)
	// Store the output int32 in the scratch buffer past VaList storage.
	outPtr := bp + 32
	libc.StoreInt32(outPtr, 0)

	var ret int32
	if e.multistream {
		if request == int32(opusccenc.OPUS_GET_MAX_BANDWIDTH_REQUEST) {
			// libopus's opus_multistream_encoder_ctl omits OPUS_GET_MAX_BANDWIDTH_REQUEST from its switch,
			// so retrieve stream 0's encoder state and query it directly.
			stPtr := bp + 48
			libc.StoreUintptr(stPtr, 0)
			ret = opusccenc.Opus_opus_multistream_encoder_ctl(
				e.tls,
				e.st,
				int32(opusccenc.OPUS_MULTISTREAM_GET_ENCODER_STATE_REQUEST),
				libc.VaList(bp, int32(0), stPtr),
			)
			if ret == opusccenc.OPUS_OK {
				subEnc := libc.LoadUintptr(stPtr)
				if subEnc != 0 {
					ret = opusccenc.Opus_opus_encoder_ctl(
						e.tls,
						subEnc,
						request,
						libc.VaList(bp, outPtr),
					)
				}
			}
		} else {
			ret = opusccenc.Opus_opus_multistream_encoder_ctl(
				e.tls,
				e.st,
				request,
				libc.VaList(bp, outPtr),
			)
		}
	} else {
		ret = opusccenc.Opus_opus_encoder_ctl(
			e.tls,
			e.st,
			request,
			libc.VaList(bp, outPtr),
		)
	}
	if ret != opusccenc.OPUS_OK {
		return 0, fmt.Errorf("%w: %s (%d)", ErrCtlFailed, opusccencErrorString(e.tls, ret), ret)
	}
	return libc.LoadInt32(outPtr), nil
}

// PreSkip returns the encoder delay in 48 kHz samples, ready to store in OpusHead.PreSkip
// (RFC 7845 Section 5.1), whatever rate the encoder runs at. Lookahead reports the same delay
// in samples at the encoder's own rate, so it is smaller by 48000/SampleRate.
func (e *Encoder) PreSkip() (int, error) {
	la, err := e.Lookahead()
	if err != nil {
		return 0, err
	}
	return la * 48000 / e.sampleRate, nil
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
			libc.VaList(bp, outPtr),
		)
	} else {
		ret = opusccenc.Opus_opus_encoder_ctl(
			e.tls,
			e.st,
			int32(opusccenc.OPUS_GET_FINAL_RANGE_REQUEST),
			libc.VaList(bp, outPtr),
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
		return 0, fmt.Errorf("%w: %s (%d)", ErrEncodeFailed, opusccencErrorString(e.tls, ret), ret)
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
		return 0, fmt.Errorf("%w: %s (%d)", ErrEncodeFailed, opusccencErrorString(e.tls, ret), ret)
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

// int32Arg narrows a caller-supplied int to the int32 the codec takes, rejecting values
// that would otherwise wrap silently (SetBitrate(1<<32 + 64000) becoming 64000).
func int32Arg(name string, v int) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("opus: %s %d out of range", name, v)
	}
	return int32(v), nil
}

// opusAllocFail is libopus's OPUS_ALLOC_FAIL, which the transpiled package does not export.
const opusAllocFail = -7
