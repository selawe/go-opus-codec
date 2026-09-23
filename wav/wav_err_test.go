package wav

import (
	"errors"
	"io"
	"testing"
)

type errWriteSeeker struct {
	writeErr error
	seekErr  error
}

func (e *errWriteSeeker) Write(p []byte) (n int, err error) {
	if e.writeErr != nil {
		return 0, e.writeErr
	}
	return len(p), nil
}

func (e *errWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	if e.seekErr != nil {
		return 0, e.seekErr
	}
	return 0, nil
}

func TestWavWriterErrors(t *testing.T) {
	myErr := errors.New("my err")

	// Error on write
	ew := &errWriteSeeker{writeErr: myErr}
	if _, err := NewWriter(ew, 48000, 2); err == nil {
		t.Error("expected err")
	}

	// Error on seek during close
	ew2 := &errWriteSeeker{seekErr: myErr}
	wr, err := NewWriter(ew2, 48000, 2)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := wr.WriteInt16PCM([]int16{0, 0}); err != nil {
		t.Fatalf("WriteInt16PCM: %v", err)
	}
	if err := wr.Close(); err == nil {
		t.Error("expected err on seek close")
	}

	// Write after close
	if err := wr.WriteInt16PCM([]int16{0, 0}); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("expected ErrClosedPipe, got %v", err)
	}

	// ErrDataTooLarge
	ew3 := &errWriteSeeker{}
	wr2, err := NewWriter(ew3, 48000, 2)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	wr2.dataBytes = uint32(maxDataBytes)
	if err := wr2.WriteInt16PCM([]int16{0, 0}); !errors.Is(err, ErrDataTooLarge) {
		t.Errorf("expected ErrDataTooLarge, got %v", err)
	}
}

type errReader struct {
	data     []byte
	pos      int
	readErr  error
	errAfter int
}

func (e *errReader) Read(p []byte) (n int, err error) {
	if e.readErr != nil && e.pos >= e.errAfter {
		return 0, e.readErr
	}
	if e.pos >= len(e.data) {
		return 0, io.EOF
	}
	n = copy(p, e.data[e.pos:])
	e.pos += n
	return n, nil
}

func TestWavReaderErrors(t *testing.T) {
	myErr := errors.New("my err")

	// Error reading RIFF
	r := &errReader{data: []byte("RI"), readErr: myErr, errAfter: 2}
	if _, err := NewReader(r); err == nil {
		t.Error("expected error on tiny header")
	}

	// Not RIFF
	r2 := &errReader{data: []byte("FFFF1234WAVEfmt ")}
	if _, err := NewReader(r2); err == nil {
		t.Error("expected err not riff")
	}

	// Valid header, EOF in data
	goodHeader := []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x02\x00\x44\xAC\x00\x00\x10\xB1\x02\x00\x04\x00\x10\x00data\x04\x00\x00\x00")
	r3 := &errReader{data: goodHeader}
	reader, err := NewReader(r3)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	pcm := make([]int16, 100)
	n, err := reader.ReadInt16PCM(pcm)
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got n=%d err=%v", n, err)
	}
}
