// Package wav implements reading and writing of RIFF/WAVE uncompressed PCM audio files.
//
// It supports 16-bit linear PCM audio across mono, stereo, and multichannel configurations.
//
// Concurrency: Types in this package (Reader and Writer) wrap io.Reader or io.WriteSeeker
// and are not safe for concurrent use by multiple goroutines without external synchronization.
package wav

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// ErrDataTooLarge is returned by Writer.WriteInt16PCM when the PCM payload would
// exceed the 4 GiB limit of a RIFF/WAVE file (32-bit chunk sizes).
var ErrDataTooLarge = errors.New("wav: PCM data exceeds the 4 GiB RIFF size limit")

// maxDataBytes is the largest data chunk whose RIFF size (data + 36 bytes of
// headers) still fits in a uint32.
const maxDataBytes = uint64(^uint32(0)) - 36

// Writer writes 16-bit PCM WAV files.
//
// It requires an io.WriteSeeker so it can patch sizes on Close.
type Writer struct {
	w          io.WriteSeeker
	bw         *bufio.Writer
	sampleRate uint32
	channels   uint16

	buf []byte

	dataBytes uint32
	start     int64 // offset of the RIFF header in w, which Close patches relative to
	closed    bool
	closeErr  error
}

// NewWriter creates a new WAV writer that outputs to an io.WriteSeeker with the given sample rate and channel count.
func NewWriter(w io.WriteSeeker, sampleRate int, channels int) (*Writer, error) {
	if channels < 1 || channels > math.MaxUint16/2 {
		return nil, fmt.Errorf("wav: invalid channel count %d", channels)
	}
	if sampleRate < 1 || uint64(sampleRate)*uint64(channels)*2 > math.MaxUint32 {
		return nil, fmt.Errorf("wav: invalid sample rate %d for %d channels", sampleRate, channels)
	}
	// A writer that cannot report its position is treated as starting at 0; Close then
	// surfaces the same seek failure when it patches the header.
	start, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		start = 0
	}
	wr := &Writer{
		start:      start,
		w:          w,
		bw:         bufio.NewWriterSize(w, 1<<20),
		sampleRate: uint32(sampleRate),
		channels:   uint16(channels),
	}
	if err := wr.writeHeaderPlaceholder(); err != nil {
		return nil, err
	}
	// Ensure header is on disk before we later Seek() to patch sizes.
	if err := wr.bw.Flush(); err != nil {
		return nil, err
	}
	return wr, nil
}

// WriteInt16PCM appends interleaved 16-bit little-endian PCM samples.
// It returns ErrDataTooLarge, without writing anything, if the samples would
// push the data chunk past the 4 GiB RIFF limit.
func (wr *Writer) WriteInt16PCM(pcm []int16) error {
	if wr.closed {
		return io.ErrClosedPipe
	}
	if len(pcm) == 0 {
		return nil
	}
	if len(pcm)%int(wr.channels) != 0 {
		return fmt.Errorf("wav: %d samples is not a whole number of %d-channel frames", len(pcm), wr.channels)
	}

	n := len(pcm) * 2
	if uint64(wr.dataBytes)+uint64(n) > maxDataBytes {
		return ErrDataTooLarge
	}
	if cap(wr.buf) < n {
		wr.buf = make([]byte, n)
	}
	buf := wr.buf[:n]
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	if _, err := wr.bw.Write(buf); err != nil {
		return err
	}
	wr.dataBytes += uint32(n)
	return nil
}

// Close flushes buffered samples and patches the RIFF and data chunk sizes.
// It does not close the underlying io.WriteSeeker.
func (wr *Writer) Close() error {
	if wr.closed {
		return wr.closeErr // a failed first Close keeps failing instead of reporting success
	}
	wr.closed = true
	wr.closeErr = wr.finish()
	return wr.closeErr
}

func (wr *Writer) finish() error {
	if wr.bw != nil {
		if err := wr.bw.Flush(); err != nil {
			return err
		}
	}

	// Patch RIFF chunk size and data chunk size.
	// RIFF size = 4 (WAVE) + (8+fmt) + (8+data)
	// We wrote: 12 + (8+16) + 8 + data
	riffSize := 4 + (8 + 16) + (8 + wr.dataBytes)

	if _, err := wr.w.Seek(wr.start+4, io.SeekStart); err != nil {
		return err
	}
	if err := binary.Write(wr.w, binary.LittleEndian, riffSize); err != nil {
		return err
	}
	if _, err := wr.w.Seek(wr.start+40, io.SeekStart); err != nil {
		return err
	}
	if err := binary.Write(wr.w, binary.LittleEndian, wr.dataBytes); err != nil {
		return err
	}

	_, err := wr.w.Seek(0, io.SeekEnd)
	return err
}

func (wr *Writer) writeHeaderPlaceholder() error {
	// Minimal PCM WAV header.
	// RIFF header
	if _, err := wr.bw.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(wr.bw, binary.LittleEndian, uint32(0)); err != nil { // placeholder
		return err
	}
	if _, err := wr.bw.Write([]byte("WAVE")); err != nil {
		return err
	}

	// fmt chunk
	if _, err := wr.bw.Write([]byte("fmt ")); err != nil {
		return err
	}
	if err := binary.Write(wr.bw, binary.LittleEndian, uint32(16)); err != nil { // PCM
		return err
	}
	if err := binary.Write(wr.bw, binary.LittleEndian, uint16(1)); err != nil { // audio format PCM
		return err
	}
	if err := binary.Write(wr.bw, binary.LittleEndian, wr.channels); err != nil {
		return err
	}
	if err := binary.Write(wr.bw, binary.LittleEndian, wr.sampleRate); err != nil {
		return err
	}
	byteRate := wr.sampleRate * uint32(wr.channels) * 2
	blockAlign := wr.channels * 2
	if err := binary.Write(wr.bw, binary.LittleEndian, byteRate); err != nil {
		return err
	}
	if err := binary.Write(wr.bw, binary.LittleEndian, blockAlign); err != nil {
		return err
	}
	if err := binary.Write(wr.bw, binary.LittleEndian, uint16(16)); err != nil { // bits per sample
		return err
	}

	// data chunk
	if _, err := wr.bw.Write([]byte("data")); err != nil {
		return err
	}
	return binary.Write(wr.bw, binary.LittleEndian, uint32(0)) // placeholder
}
