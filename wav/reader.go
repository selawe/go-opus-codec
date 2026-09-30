package wav

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

var (
	ErrNotWAV         = errors.New("wav: not a RIFF/WAVE file")
	ErrUnsupportedWAV = errors.New("wav: unsupported WAV format")
)

// Standard WAV audio format tags.
const (
	FormatPCM        = 1
	FormatIEEEFloat  = 3
	FormatExtensible = 0xFFFE
)

// MaxSampleRate is the highest sample rate NewReader accepts. Headers are
// untrusted, and absurd rates would otherwise drive downstream resampler sizing.
const MaxSampleRate = 768000

// Reader reads PCM and IEEE floating-point little-endian WAV data, including RF64 files.
//
// Supported bit depths: 8-bit unsigned PCM, 16-bit signed PCM, 24-bit signed PCM,
// 32-bit signed PCM, and 32-bit IEEE float.
//
// Concurrency: Reader wraps an io.Reader and is not safe for concurrent use without external synchronization.
type Reader struct {
	br *bufio.Reader

	sampleRate    int
	channels      int
	bitsPerSample int
	format        uint16

	buf           []byte
	dataRemaining uint64
	// unknownLen marks a streamed file whose writer could not fill in the
	// data size (0xFFFFFFFF without ds64, or 0 with a zero RIFF size): read until the input ends.
	unknownLen bool

	hasDS64      bool
	ds64DataSize uint64
}

// NewReader creates a new WAV reader parsing the RIFF or RF64 header from r.
func NewReader(r io.Reader) (*Reader, error) {
	wr := &Reader{br: bufio.NewReaderSize(r, 1<<20)}
	if err := wr.readHeader(); err != nil {
		return nil, err
	}
	return wr, nil
}

// SampleRate returns the sample rate in Hz parsed from the WAV header.
func (r *Reader) SampleRate() int { return r.sampleRate }

// Channels returns the number of channels parsed from the WAV header.
func (r *Reader) Channels() int { return r.channels }

// BitsPerSample returns the bit depth per sample (8, 16, 24, or 32).
func (r *Reader) BitsPerSample() int { return r.bitsPerSample }

// Format returns the audio format code (FormatPCM = 1 or FormatIEEEFloat = 3).
func (r *Reader) Format() uint16 { return r.format }

// ReadInt16PCM reads whole frames into dst, at most len(dst) samples. It returns the
// number of samples read, always a multiple of Channels(). A dst shorter than one frame
// yields io.ErrShortBuffer, and a trailing partial frame in the file is dropped.
//
// Samples are converted to signed 16-bit PCM regardless of the source bit depth.
func (r *Reader) ReadInt16PCM(dst []int16) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	nFrames := len(dst) / r.channels
	if nFrames == 0 {
		return 0, io.ErrShortBuffer
	}

	bytesPerSample := r.bitsPerSample / 8
	bytesPerFrame := r.channels * bytesPerSample

	if !r.unknownLen {
		if r.dataRemaining == 0 {
			return 0, io.EOF
		}
		maxFrames := int(r.dataRemaining / uint64(bytesPerFrame))
		if maxFrames <= 0 {
			r.dataRemaining = 0
			return 0, io.EOF
		}
		if nFrames > maxFrames {
			nFrames = maxFrames
		}
	}

	nBytes := nFrames * bytesPerFrame
	if cap(r.buf) < nBytes {
		r.buf = make([]byte, nBytes)
	}
	buf := r.buf[:nBytes]
	nRead, err := io.ReadFull(r.br, buf)
	framesRead := nRead / bytesPerFrame
	samplesRead := framesRead * r.channels

	switch r.bitsPerSample {
	case 8:
		for i := 0; i < samplesRead; i++ {
			dst[i] = int16(int32(buf[i])-128) << 8
		}
	case 16:
		for i := 0; i < samplesRead; i++ {
			dst[i] = int16(binary.LittleEndian.Uint16(buf[i*2:]))
		}
	case 24:
		for i := 0; i < samplesRead; i++ {
			s := int32(buf[i*3]) | int32(buf[i*3+1])<<8 | int32(int8(buf[i*3+2]))<<16
			dst[i] = int16(s >> 8)
		}
	case 32:
		if r.format == FormatIEEEFloat {
			for i := 0; i < samplesRead; i++ {
				bits := binary.LittleEndian.Uint32(buf[i*4:])
				f := math.Float32frombits(bits)
				if math.IsNaN(float64(f)) {
					dst[i] = 0
				} else if f >= 1.0 {
					dst[i] = 32767
				} else if f <= -1.0 {
					dst[i] = -32768
				} else if f >= 0 {
					dst[i] = int16(math.Round(float64(f * 32767.0)))
				} else {
					dst[i] = int16(math.Round(float64(f * 32768.0)))
				}
			}
		} else {
			for i := 0; i < samplesRead; i++ {
				s := int32(binary.LittleEndian.Uint32(buf[i*4:]))
				dst[i] = int16(s >> 16)
			}
		}
	}

	if !r.unknownLen {
		r.dataRemaining -= uint64(framesRead * bytesPerFrame)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		r.dataRemaining = 0
		r.unknownLen = false
		if samplesRead > 0 {
			return samplesRead, nil
		}
		return 0, io.EOF
	}
	if err != nil {
		return samplesRead, err
	}
	return samplesRead, nil
}

// ReadFloat32PCM reads whole frames into dst, at most len(dst) samples. It returns the
// number of samples read, always a multiple of Channels(). A dst shorter than one frame
// yields io.ErrShortBuffer, and a trailing partial frame in the file is dropped.
//
// Samples are returned as normalized float32 in [-1.0, 1.0].
func (r *Reader) ReadFloat32PCM(dst []float32) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	nFrames := len(dst) / r.channels
	if nFrames == 0 {
		return 0, io.ErrShortBuffer
	}

	bytesPerSample := r.bitsPerSample / 8
	bytesPerFrame := r.channels * bytesPerSample

	if !r.unknownLen {
		if r.dataRemaining == 0 {
			return 0, io.EOF
		}
		maxFrames := int(r.dataRemaining / uint64(bytesPerFrame))
		if maxFrames <= 0 {
			r.dataRemaining = 0
			return 0, io.EOF
		}
		if nFrames > maxFrames {
			nFrames = maxFrames
		}
	}

	nBytes := nFrames * bytesPerFrame
	if cap(r.buf) < nBytes {
		r.buf = make([]byte, nBytes)
	}
	buf := r.buf[:nBytes]
	nRead, err := io.ReadFull(r.br, buf)
	framesRead := nRead / bytesPerFrame
	samplesRead := framesRead * r.channels

	switch r.bitsPerSample {
	case 8:
		for i := 0; i < samplesRead; i++ {
			dst[i] = float32(int32(buf[i])-128) / 128.0
		}
	case 16:
		for i := 0; i < samplesRead; i++ {
			s := int16(binary.LittleEndian.Uint16(buf[i*2:]))
			dst[i] = float32(s) / 32768.0
		}
	case 24:
		for i := 0; i < samplesRead; i++ {
			s := int32(buf[i*3]) | int32(buf[i*3+1])<<8 | int32(int8(buf[i*3+2]))<<16
			dst[i] = float32(s) / 8388608.0
		}
	case 32:
		if r.format == FormatIEEEFloat {
			for i := 0; i < samplesRead; i++ {
				bits := binary.LittleEndian.Uint32(buf[i*4:])
				f := math.Float32frombits(bits)
				if math.IsNaN(float64(f)) {
					dst[i] = 0
				} else {
					dst[i] = f
				}
			}
		} else {
			for i := 0; i < samplesRead; i++ {
				s := int32(binary.LittleEndian.Uint32(buf[i*4:]))
				dst[i] = float32(s) / 2147483648.0
			}
		}
	}

	if !r.unknownLen {
		r.dataRemaining -= uint64(framesRead * bytesPerFrame)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		r.dataRemaining = 0
		r.unknownLen = false
		if samplesRead > 0 {
			return samplesRead, nil
		}
		return 0, io.EOF
	}
	if err != nil {
		return samplesRead, err
	}
	return samplesRead, nil
}

func (r *Reader) readHeader() error {
	var header [12]byte
	if _, err := io.ReadFull(r.br, header[:]); err != nil {
		return err
	}
	riffID := string(header[0:4])
	isRF64 := riffID == "RF64" || riffID == "BW64"
	if riffID != "RIFF" && !isRF64 {
		return ErrNotWAV
	}
	if string(header[8:12]) != "WAVE" {
		return ErrNotWAV
	}

	var haveFmt bool

	for {
		var chdr [8]byte
		if _, err := io.ReadFull(r.br, chdr[:]); err != nil {
			return err
		}
		id := string(chdr[0:4])
		sz := binary.LittleEndian.Uint32(chdr[4:8])

		switch id {
		case "ds64":
			if sz < 28 {
				return fmt.Errorf("%w: ds64 chunk too short (%d bytes)", ErrUnsupportedWAV, sz)
			}
			var dsBuf [28]byte
			if _, err := io.ReadFull(r.br, dsBuf[:]); err != nil {
				return err
			}
			riffSize := binary.LittleEndian.Uint64(dsBuf[0:8])
			dataSize := binary.LittleEndian.Uint64(dsBuf[8:16])
			sampleCount := binary.LittleEndian.Uint64(dsBuf[16:24])
			_ = riffSize
			_ = sampleCount
			rem := int64(sz) - 28
			if rem > 0 {
				if _, err := io.CopyN(io.Discard, r.br, rem); err != nil {
					return err
				}
			}
			if sz%2 == 1 {
				if _, err := r.br.ReadByte(); err != nil {
					return err
				}
			}
			r.hasDS64 = true
			r.ds64DataSize = dataSize

		case "fmt ":
			if sz < 16 {
				return fmt.Errorf("%w: fmt chunk too short", ErrUnsupportedWAV)
			}
			if sz > 64<<10 {
				return fmt.Errorf("%w: fmt chunk too large (%d bytes)", ErrUnsupportedWAV, sz)
			}
			buf := make([]byte, sz)
			if _, err := io.ReadFull(r.br, buf); err != nil {
				return err
			}
			if sz%2 == 1 {
				if _, err := r.br.ReadByte(); err != nil {
					return err
				}
			}
			audioFormat := binary.LittleEndian.Uint16(buf[0:2])
			channels := binary.LittleEndian.Uint16(buf[2:4])
			sampleRate := binary.LittleEndian.Uint32(buf[4:8])
			blockAlign := binary.LittleEndian.Uint16(buf[12:14])
			bitsPerSample := binary.LittleEndian.Uint16(buf[14:16])

			if channels == 0 {
				return fmt.Errorf("%w: channels=0", ErrUnsupportedWAV)
			}
			if sampleRate == 0 {
				return fmt.Errorf("%w: sample rate=0", ErrUnsupportedWAV)
			}
			if sampleRate > MaxSampleRate {
				return fmt.Errorf("%w: sample rate=%d exceeds %d", ErrUnsupportedWAV, sampleRate, MaxSampleRate)
			}

			var effectiveFormat uint16
			switch audioFormat {
			case FormatPCM:
				effectiveFormat = FormatPCM
				if bitsPerSample != 8 && bitsPerSample != 16 && bitsPerSample != 24 && bitsPerSample != 32 {
					return fmt.Errorf("%w: PCM bits per sample=%d (supported: 8, 16, 24, 32)", ErrUnsupportedWAV, bitsPerSample)
				}
			case FormatIEEEFloat:
				effectiveFormat = FormatIEEEFloat
				if bitsPerSample != 32 {
					return fmt.Errorf("%w: IEEE float bits per sample=%d (supported: 32)", ErrUnsupportedWAV, bitsPerSample)
				}
			case FormatExtensible:
				if sz < 40 {
					return fmt.Errorf("%w: extensible fmt chunk too short (%d bytes)", ErrUnsupportedWAV, sz)
				}
				validBits := binary.LittleEndian.Uint16(buf[18:20])
				if validBits != 0 && validBits > bitsPerSample {
					return fmt.Errorf("%w: valid bits per sample (%d) exceeds container size (%d)", ErrUnsupportedWAV, validBits, bitsPerSample)
				}
				// Standard KSDATAFORMAT_SUBTYPE suffix: \x00\x00\x10\x00\x80\x00\x00\xAA\x00\x38\x9B\x71
				ksSuffix := []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}
				if !bytes.Equal(buf[28:40], ksSuffix) {
					return fmt.Errorf("%w: unknown extensible subformat GUID", ErrUnsupportedWAV)
				}
				subFormat := binary.LittleEndian.Uint32(buf[24:28])
				switch subFormat {
				case uint32(FormatPCM):
					effectiveFormat = FormatPCM
					if bitsPerSample != 8 && bitsPerSample != 16 && bitsPerSample != 24 && bitsPerSample != 32 {
						return fmt.Errorf("%w: extensible PCM bits per sample=%d", ErrUnsupportedWAV, bitsPerSample)
					}
				case uint32(FormatIEEEFloat):
					effectiveFormat = FormatIEEEFloat
					if bitsPerSample != 32 {
						return fmt.Errorf("%w: extensible IEEE float bits per sample=%d", ErrUnsupportedWAV, bitsPerSample)
					}
				default:
					return fmt.Errorf("%w: extensible subformat=%d", ErrUnsupportedWAV, subFormat)
				}
			default:
				return fmt.Errorf("%w: audio format=%d", ErrUnsupportedWAV, audioFormat)
			}

			expectedBlockAlign := channels * (bitsPerSample / 8)
			if blockAlign != expectedBlockAlign {
				return fmt.Errorf("%w: invalid block align %d (expected %d for %d ch %d-bit)", ErrUnsupportedWAV, blockAlign, expectedBlockAlign, channels, bitsPerSample)
			}

			r.sampleRate = int(sampleRate)
			r.channels = int(channels)
			r.bitsPerSample = int(bitsPerSample)
			r.format = effectiveFormat
			haveFmt = true

		case "data":
			if !haveFmt {
				return fmt.Errorf("%w: data before fmt", ErrUnsupportedWAV)
			}
			if r.hasDS64 && (sz == 0xFFFFFFFF || isRF64) {
				r.dataRemaining = r.ds64DataSize
			} else {
				r.dataRemaining = uint64(sz)
			}
			riffSize := binary.LittleEndian.Uint32(header[4:8])
			r.unknownLen = (!r.hasDS64 && sz == 0xFFFFFFFF) || (r.dataRemaining == 0 && (riffSize == 0 || riffSize == 0xFFFFFFFF))
			return nil

		default:
			// Skip unknown chunk (plus padding byte if needed).
			if _, err := io.CopyN(io.Discard, r.br, int64(sz)); err != nil {
				return err
			}
			if sz%2 == 1 {
				if _, err := r.br.ReadByte(); err != nil {
					return err
				}
			}
		}
	}
}
