package opusgo

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
	"github.com/selawe/go-opus-codec/wav"
)

// Opus application mode constants re-exported from the opus package.
const (
	ApplicationVoIP               = opus.ApplicationVoIP
	ApplicationAudio              = opus.ApplicationAudio
	ApplicationRestrictedLowDelay = opus.ApplicationRestrictedLowDelay
)

// EncodeOptions configures WAV to Ogg Opus encoding.
type EncodeOptions struct {
	// Bitrate is the target bitrate in bits per second (e.g. 64000, 128000).
	// If <= 0, the default of 64000 (64 kbps) is used.
	Bitrate int

	// CBR forces Constant Bitrate encoding. By default (false), Variable Bitrate (VBR) is enabled.
	CBR bool

	// Complexity sets the encoder computational complexity from 0 to 10.
	// Defaults to 10 if <= 0 or > 10.
	Complexity int

	// Application specifies the encoder application mode (ApplicationAudio, ApplicationVoIP,
	// ApplicationRestrictedLowDelay). Defaults to ApplicationAudio.
	Application int

	// FrameSizeMS specifies frame duration in milliseconds (5, 10, 20, 40, 60).
	// Defaults to 20 ms.
	FrameSizeMS int

	// Vendor is the vendor string for the OpusTags header. Defaults to "opusgo".
	Vendor string

	// Comments are user comments for the OpusTags header (e.g. "TITLE=Song").
	// If empty, default ENCODER and ENCODED tags are written.
	Comments []string

	// Serial is the Ogg bitstream serial number. If 0, a random serial number is generated.
	Serial uint32
}

// DefaultEncodeOptions returns standard encoding options (64 kbps VBR, 20ms frames, audio mode, complexity 10).
func DefaultEncodeOptions() EncodeOptions {
	return EncodeOptions{
		Bitrate:     64000,
		CBR:         false,
		Complexity:  10,
		Application: ApplicationAudio,
		FrameSizeMS: 20,
		Vendor:      "opusgo",
	}
}

// EncodeWAVToOggOpus reads a 48kHz 16-bit linear PCM WAV stream from wavReader,
// encodes it to Opus audio, and multiplexes the result into an Ogg container written to oggWriter.
// If opts is nil, DefaultEncodeOptions() is used.
func EncodeWAVToOggOpus(wavReader io.Reader, oggWriter io.Writer, opts *EncodeOptions) error {
	if wavReader == nil {
		return errors.New("wavReader cannot be nil")
	}
	if oggWriter == nil {
		return errors.New("oggWriter cannot be nil")
	}

	var cfg EncodeOptions
	if opts != nil {
		cfg = *opts
	} else {
		cfg = DefaultEncodeOptions()
	}

	bitrate := cfg.Bitrate
	if bitrate <= 0 {
		bitrate = 64000
	}
	vbr := !cfg.CBR
	complexity := cfg.Complexity
	if complexity <= 0 || complexity > 10 {
		complexity = 10
	}
	app := cfg.Application
	if app == 0 {
		app = ApplicationAudio
	}
	frameMS := cfg.FrameSizeMS
	if frameMS == 0 {
		frameMS = 20
	}
	vendor := cfg.Vendor
	if vendor == "" {
		vendor = "opusgo"
	}
	serial := cfg.Serial
	if serial == 0 {
		serial = randomSerial()
	}
	comments := cfg.Comments
	if len(comments) == 0 {
		comments = []string{
			"ENCODER=opusgo",
			"ENCODED=" + time.Now().UTC().Format(time.RFC3339),
		}
	}

	frameSize, err := frameSizeFromMS(frameMS)
	if err != nil {
		return err
	}

	wr, err := wav.NewReader(wavReader)
	if err != nil {
		return fmt.Errorf("wav reader: %w", err)
	}
	if wr.SampleRate() != 48000 {
		return fmt.Errorf("unsupported sample rate %d: only 48000 Hz WAV supported", wr.SampleRate())
	}
	if wr.Channels() < 1 || wr.Channels() > 2 {
		return fmt.Errorf("unsupported channel count %d: only mono (1) and stereo (2) supported", wr.Channels())
	}

	enc, err := opus.NewEncoder(wr.SampleRate(), wr.Channels(), app)
	if err != nil {
		return fmt.Errorf("opus encoder: %w", err)
	}
	defer enc.Close()

	if err := enc.SetBitrate(bitrate); err != nil {
		return fmt.Errorf("set bitrate: %w", err)
	}
	if err := enc.SetVBR(vbr); err != nil {
		return fmt.Errorf("set vbr: %w", err)
	}
	if err := enc.SetComplexity(complexity); err != nil {
		return fmt.Errorf("set complexity: %w", err)
	}

	lookahead, err := enc.Lookahead()
	if err != nil {
		return fmt.Errorf("opus lookahead: %w", err)
	}

	bw, ok := oggWriter.(*bufio.Writer)
	if !ok {
		bw = bufio.NewWriterSize(oggWriter, 64*1024)
		defer bw.Flush()
	}

	pw := ogg.NewPacketWriter(bw, serial)

	head := ogg.OpusHead{
		Version:              1,
		Channels:             uint8(wr.Channels()),
		PreSkip:              uint16(lookahead),
		InputSampleRate:      48000,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	headPkt, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		return fmt.Errorf("build opus head packet: %w", err)
	}

	tags := ogg.OpusTags{
		Vendor:   vendor,
		Comments: comments,
	}
	tagsPkt, err := ogg.BuildOpusTagsPacket(tags)
	if err != nil {
		return fmt.Errorf("build opus tags packet: %w", err)
	}

	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		return fmt.Errorf("ogg write head: %w", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		return fmt.Errorf("ogg write tags: %w", err)
	}

	frameSamples := frameSize * wr.Channels()
	pcm := make([]int16, frameSamples)
	packet := make([]byte, 4000)

	var inputSamplesPerCh uint64
	var encodedSamplesPerCh uint64
	var eofReached bool
	pending := make([]int16, 0, 2*frameSamples)
	readBuf := make([]int16, frameSamples)

	for {
		for len(pending) < frameSamples && !eofReached {
			n, rerr := wr.ReadInt16PCM(readBuf)
			if rerr != nil && !errors.Is(rerr, io.EOF) {
				return fmt.Errorf("wav read: %w", rerr)
			}
			if n > 0 {
				if n%wr.Channels() != 0 {
					return fmt.Errorf("wav: sample count %d not a multiple of channels %d", n, wr.Channels())
				}
				pending = append(pending, readBuf[:n]...)
				inputSamplesPerCh += uint64(n / wr.Channels())
			}
			if errors.Is(rerr, io.EOF) {
				eofReached = true
			}
		}

		if !eofReached {
			// We have a full frame of input audio available
			copy(pcm, pending[:frameSamples])
			pending = pending[frameSamples:]

			nBytes, err := enc.Encode(pcm, frameSize, packet)
			if err != nil {
				return fmt.Errorf("opus encode: %w", err)
			}
			encodedSamplesPerCh += uint64(frameSize)
			granule := uint64(head.PreSkip) + encodedSamplesPerCh

			if err := pw.WritePacket(packet[:nBytes], granule, false, false); err != nil {
				return fmt.Errorf("ogg write: %w", err)
			}
		} else {
			// EOF reached. Per RFC 7845 Section 4, the encoder must encode enough silence
			// padding so that all original input samples pass through the encoder's lookahead delay.
			if inputSamplesPerCh == 0 {
				break
			}
			targetSamplesPerCh := ((inputSamplesPerCh + uint64(lookahead) + uint64(frameSize) - 1) / uint64(frameSize)) * uint64(frameSize)
			eosGranule := uint64(head.PreSkip) + inputSamplesPerCh

			for encodedSamplesPerCh < targetSamplesPerCh {
				isLast := (encodedSamplesPerCh + uint64(frameSize) >= targetSamplesPerCh)

				toCopy := len(pending)
				if toCopy > frameSamples {
					toCopy = frameSamples
				}
				copy(pcm[:toCopy], pending[:toCopy])
				for i := toCopy; i < frameSamples; i++ {
					pcm[i] = 0
				}
				if toCopy > 0 {
					pending = pending[toCopy:]
				}

				nBytes, err := enc.Encode(pcm, frameSize, packet)
				if err != nil {
					return fmt.Errorf("opus encode: %w", err)
				}
				encodedSamplesPerCh += uint64(frameSize)

				granule := uint64(head.PreSkip) + encodedSamplesPerCh
				if granule > eosGranule || isLast {
					granule = eosGranule
				}

				if err := pw.WritePacket(packet[:nBytes], granule, false, isLast); err != nil {
					return fmt.Errorf("ogg write: %w", err)
				}
			}
			break
		}
	}

	if err := pw.Flush(); err != nil {
		return fmt.Errorf("ogg flush: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("writer flush: %w", err)
	}
	return nil
}

// DecodeOggOpusToWAV reads an Ogg Opus stream from oggReader and writes
// a 48kHz 16-bit linear PCM WAV stream to wavWriter.
// wavWriter must implement io.WriteSeeker so WAV header chunk sizes can be finalized on completion.
func DecodeOggOpusToWAV(oggReader io.Reader, wavWriter io.WriteSeeker) error {
	if oggReader == nil {
		return errors.New("oggReader cannot be nil")
	}
	if wavWriter == nil {
		return errors.New("wavWriter cannot be nil")
	}

	r, err := ogg.NewOpusReader(oggReader)
	if err != nil {
		return fmt.Errorf("ogg opus reader: %w", err)
	}

	dec, err := opus.NewDecoderFromHead(r.Head)
	if err != nil {
		return fmt.Errorf("opus decoder: %w", err)
	}
	defer dec.Close()

	ww, err := wav.NewWriter(wavWriter, 48000, int(r.Head.Channels))
	if err != nil {
		return fmt.Errorf("wav writer: %w", err)
	}
	defer ww.Close()

	maxFrame := 5760
	channels := int(r.Head.Channels)
	pcm := make([]int16, maxFrame*channels)

	preSkipRemaining := int(r.Head.PreSkip)
	totalSamplesDecoded := uint64(0)

	for {
		pkt, err := r.ReadAudioPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("ogg read audio packet: %w", err)
		}

		n, err := dec.Decode(pkt.Data, pcm, maxFrame, false)
		if err != nil {
			return fmt.Errorf("opus decode: %w", err)
		}

		totalSamplesDecoded += uint64(n)
		frames := pcm[:n*channels]

		// Apply OpusHead pre-skip in samples per channel.
		if preSkipRemaining > 0 {
			skip := preSkipRemaining
			if skip > n {
				skip = n
			}
			preSkipRemaining -= skip

			drop := skip * channels
			if drop >= len(frames) {
				continue
			}
			frames = frames[drop:]
		}

		// RFC 7845 Section 4: If packet has a valid granule position (especially at EOS),
		// trim any trailing excess samples beyond the granule position.
		if pkt.GranuleValid && totalSamplesDecoded > pkt.GranulePos {
			excess := totalSamplesDecoded - pkt.GranulePos
			excessSamples := int(excess) * channels
			if excessSamples < len(frames) {
				frames = frames[:len(frames)-excessSamples]
			} else {
				frames = nil
			}
		}

		if len(frames) > 0 {
			if err := ww.WriteInt16PCM(frames); err != nil {
				return fmt.Errorf("wav write: %w", err)
			}
		}
	}

	if err := ww.Close(); err != nil {
		return fmt.Errorf("wav close: %w", err)
	}
	return nil
}

// ConvertWAVFileToOggOpus encodes a WAV file at srcPath to an Ogg Opus file at dstPath.
func ConvertWAVFileToOggOpus(srcPath, dstPath string, opts *EncodeOptions) error {
	inF, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer inF.Close()

	outF, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer func() {
		_ = outF.Close()
	}()

	if err := EncodeWAVToOggOpus(inF, outF, opts); err != nil {
		return err
	}
	return outF.Close()
}

// ConvertOggOpusFileToWAV decodes an Ogg Opus file at srcPath to a WAV file at dstPath.
func ConvertOggOpusFileToWAV(srcPath, dstPath string) error {
	inF, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer inF.Close()

	outF, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer func() {
		_ = outF.Close()
	}()

	if err := DecodeOggOpusToWAV(inF, outF); err != nil {
		return err
	}
	return outF.Close()
}

func frameSizeFromMS(ms int) (int, error) {
	switch ms {
	case 5:
		return 240, nil
	case 10:
		return 480, nil
	case 20:
		return 960, nil
	case 40:
		return 1920, nil
	case 60:
		return 2880, nil
	default:
		return 0, fmt.Errorf("unsupported frame duration %d ms (must be 5, 10, 20, 40, or 60)", ms)
	}
}

func randomSerial() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return binary.LittleEndian.Uint32(b[:])
	}
	return uint32(time.Now().UnixNano())
}
