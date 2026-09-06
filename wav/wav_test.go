package wav

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type memoryWriteSeeker struct {
	buf []byte
	pos int64
}

func (m *memoryWriteSeeker) Write(p []byte) (n int, err error) {
	end := m.pos + int64(len(p))
	if end > int64(len(m.buf)) {
		newBuf := make([]byte, end)
		copy(newBuf, m.buf)
		m.buf = newBuf
	}
	copy(m.buf[m.pos:], p)
	m.pos = end
	return len(p), nil
}

func (m *memoryWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		m.pos = offset
	case io.SeekCurrent:
		m.pos += offset
	case io.SeekEnd:
		m.pos = int64(len(m.buf)) + offset
	default:
		return 0, errors.New("invalid whence")
	}
	return m.pos, nil
}

func TestWAVRoundtripStereo(t *testing.T) {
	ws := &memoryWriteSeeker{}
	writer, err := NewWriter(ws, 48000, 2)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	samples := make([]int16, 960*2) // 20ms of stereo
	for i := range samples {
		samples[i] = int16(i * 10)
	}

	if err := writer.WriteInt16PCM(samples); err != nil {
		t.Fatalf("WriteInt16PCM: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read back
	reader, err := NewReader(bytes.NewReader(ws.buf))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if reader.SampleRate() != 48000 {
		t.Fatalf("expected sample rate 48000, got %d", reader.SampleRate())
	}
	if reader.Channels() != 2 {
		t.Fatalf("expected 2 channels, got %d", reader.Channels())
	}

	readSamples := make([]int16, len(samples))
	n, err := reader.ReadInt16PCM(readSamples)
	if err != nil {
		t.Fatalf("ReadInt16PCM: %v", err)
	}
	if n != len(samples) {
		t.Fatalf("expected to read %d samples, got %d", len(samples), n)
	}

	for i := range samples {
		if readSamples[i] != samples[i] {
			t.Fatalf("sample mismatch at index %d: want %d, got %d", i, samples[i], readSamples[i])
		}
	}

	// Next read should return EOF
	n, err = reader.ReadInt16PCM(make([]int16, 10))
	if !errors.Is(err, io.EOF) || n != 0 {
		t.Fatalf("expected EOF on finished reader, got n=%d err=%v", n, err)
	}
}

func TestWAVRoundtripMono(t *testing.T) {
	ws := &memoryWriteSeeker{}
	writer, err := NewWriter(ws, 16000, 1)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	samples := make([]int16, 320)
	for i := range samples {
		samples[i] = int16(-i * 5)
	}

	if err := writer.WriteInt16PCM(samples); err != nil {
		t.Fatalf("WriteInt16PCM: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(ws.buf))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if reader.SampleRate() != 16000 {
		t.Fatalf("expected 16000, got %d", reader.SampleRate())
	}
	if reader.Channels() != 1 {
		t.Fatalf("expected 1 channel, got %d", reader.Channels())
	}

	readBuf := make([]int16, 1000)
	n, err := reader.ReadInt16PCM(readBuf)
	if err != nil {
		t.Fatalf("ReadInt16PCM: %v", err)
	}
	if n != len(samples) {
		t.Fatalf("read %d samples, want %d", n, len(samples))
	}
}

func TestWAVInvalidHeader(t *testing.T) {
	// Not RIFF
	badData := []byte("NOT_A_RIFF_FILE_AT_ALL_HERE")
	_, err := NewReader(bytes.NewReader(badData))
	if !errors.Is(err, ErrNotWAV) {
		t.Fatalf("expected ErrNotWAV, got %v", err)
	}

	// Truncated header
	_, err = NewReader(bytes.NewReader([]byte("RIFF")))
	if err == nil {
		t.Fatal("expected error on truncated data, got nil")
	}
}
