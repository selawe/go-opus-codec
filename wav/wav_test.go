package wav

import (
	"bytes"
	"encoding/binary"
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

func TestWAVReader_OddFmtChunk(t *testing.T) {
	// Craft a WAV file with an odd-sized fmt chunk (17 bytes: 16-byte PCM header + 1-byte extra)
	// followed by the mandatory 1-byte RIFF word padding, then a data chunk.
	var b bytes.Buffer
	b.WriteString("RIFF")
	b.Write([]byte{0, 0, 0, 0}) // placeholder for RIFF size
	b.WriteString("WAVE")

	// "fmt " chunk with size 17
	b.WriteString("fmt ")
	b.Write([]byte{17, 0, 0, 0})      // sz = 17
	b.Write([]byte{1, 0})             // audio format = 1 (PCM)
	b.Write([]byte{1, 0})             // channels = 1
	b.Write([]byte{0x80, 0x3e, 0, 0}) // sample rate = 16000
	b.Write([]byte{0x00, 0x7d, 0, 0}) // byte rate = 32000
	b.Write([]byte{2, 0})             // block align = 2
	b.Write([]byte{16, 0})            // bits per sample = 16
	b.WriteByte(0)                    // 17th byte (extra byte)
	b.WriteByte(0)                    // RIFF word padding byte (must be skipped)

	// "data" chunk with 4 bytes (2 int16 samples: 1234, 5678)
	b.WriteString("data")
	b.Write([]byte{4, 0, 0, 0})
	b.Write([]byte{0xd2, 0x04}) // 1234
	b.Write([]byte{0x2e, 0x16}) // 5678

	r, err := NewReader(bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatalf("NewReader with odd fmt chunk failed: %v", err)
	}

	if r.SampleRate() != 16000 || r.Channels() != 1 {
		t.Fatalf("unexpected rate/channels: rate=%d, ch=%d", r.SampleRate(), r.Channels())
	}

	samples := make([]int16, 2)
	n, err := r.ReadInt16PCM(samples)
	if err != nil {
		t.Fatalf("ReadInt16PCM failed: %v", err)
	}
	if n != 2 || samples[0] != 1234 || samples[1] != 5678 {
		t.Fatalf("samples mismatch: n=%d, samples=%v", n, samples)
	}
}

func TestWAVReader_OOMProtection(t *testing.T) {
	// Craft a WAV file with an excessively large fmt chunk size (e.g. 100KB or 4GB)
	var b bytes.Buffer
	b.WriteString("RIFF")
	b.Write([]byte{0, 0, 0, 0})
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	b.Write([]byte{0xff, 0xff, 0xff, 0xff}) // 4GB size

	_, err := NewReader(bytes.NewReader(b.Bytes()))
	if err == nil {
		t.Fatal("expected error on malicious 4GB fmt chunk size, got nil")
	}
	if !errors.Is(err, ErrUnsupportedWAV) {
		t.Fatalf("expected ErrUnsupportedWAV, got: %v", err)
	}
}

func BenchmarkWAVReader_ReadInt16PCM(b *testing.B) {
	ws := &memoryWriteSeeker{}
	writer, err := NewWriter(ws, 48000, 2)
	if err != nil {
		b.Fatalf("NewWriter: %v", err)
	}
	pcm := make([]int16, 960*2)
	for i := 0; i < 50; i++ {
		_ = writer.WriteInt16PCM(pcm)
	}
	_ = writer.Close()

	wavBytes := ws.buf
	dst := make([]int16, 960*2)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader, _ := NewReader(bytes.NewReader(wavBytes))
		for {
			n, err := reader.ReadInt16PCM(dst)
			if n == 0 || err != nil {
				break
			}
		}
	}
}

type discardSeeker struct{ pos int64 }

func (d *discardSeeker) Write(p []byte) (int, error) { d.pos += int64(len(p)); return len(p), nil }
func (d *discardSeeker) Seek(off int64, whence int) (int64, error) {
	if whence == io.SeekStart {
		d.pos = off
	}
	return d.pos, nil
}

func TestWriterRejectsSizeOverflow(t *testing.T) {
	w, err := NewWriter(&discardSeeker{}, 48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.dataBytes = uint32(maxDataBytes) - 2 // room for exactly one more sample
	if err := w.WriteInt16PCM([]int16{1}); err != nil {
		t.Fatalf("write within limit: %v", err)
	}
	before := w.dataBytes
	if err := w.WriteInt16PCM([]int16{1}); !errors.Is(err, ErrDataTooLarge) {
		t.Fatalf("got %v, want ErrDataTooLarge", err)
	}
	if w.dataBytes != before {
		t.Fatalf("dataBytes changed on rejected write: %d -> %d", before, w.dataBytes)
	}
}

func TestWAVReader_SampleRateZero(t *testing.T) {
	// Minimal WAV with sampleRate=0
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))  // format PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))  // channels 1
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))  // sampleRate 0
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))  // byteRate
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))  // blockAlign
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16)) // bitsPerSample
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))

	_, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error for sampleRate=0, got nil")
	}
}

func TestWAVReader_Extensible(t *testing.T) {
	// Build WAVE_FORMAT_EXTENSIBLE 6-channel 48kHz PCM WAV
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(40+36))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(40))     // extensible size 40
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xFFFE)) // WAVE_FORMAT_EXTENSIBLE
	_ = binary.Write(&buf, binary.LittleEndian, uint16(6))      // channels 6
	_ = binary.Write(&buf, binary.LittleEndian, uint32(48000))  // sampleRate
	_ = binary.Write(&buf, binary.LittleEndian, uint32(48000*6*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(12))   // blockAlign
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))   // bitsPerSample
	_ = binary.Write(&buf, binary.LittleEndian, uint16(22))   // cbSize
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))   // validBitsPerSample
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0x3F)) // channelMask (5.1)
	// SubFormat GUID for PCM: {00000001-0000-0010-8000-00aa00389b71}
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // subFormat PCM
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71})
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(12)) // 1 frame of 6 samples
	for i := int16(1); i <= 6; i++ {
		_ = binary.Write(&buf, binary.LittleEndian, i)
	}

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed for extensible WAV: %v", err)
	}
	if r.Channels() != 6 {
		t.Errorf("expected 6 channels, got %d", r.Channels())
	}
	if r.SampleRate() != 48000 {
		t.Errorf("expected 48000 Hz, got %d", r.SampleRate())
	}
	samples := make([]int16, 6)
	n, err := r.ReadInt16PCM(samples)
	if err != nil {
		t.Fatalf("ReadInt16PCM: %v", err)
	}
	if n != 6 {
		t.Fatalf("expected 6 samples read, got %d", n)
	}
}

func TestWAVReader_TruncatedData(t *testing.T) {
	// Build WAV header claiming 1000 samples (2000 bytes) of data, but only provide 10 samples (20 bytes)
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(2036))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(48000))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(96000))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(2000)) // claims 1000 samples
	for i := int16(1); i <= 10; i++ {
		_ = binary.Write(&buf, binary.LittleEndian, i) // only 10 samples provided
	}

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	dst := make([]int16, 500)
	n, err := r.ReadInt16PCM(dst)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected error on partial read: %v", err)
	}
	if n != 10 {
		t.Fatalf("expected 10 samples from partial read before EOF, got %d", n)
	}
}

func TestWAVReader_SampleRateTooHigh(t *testing.T) {
	for _, rate := range []uint32{MaxSampleRate + 1, 4294967295} {
		var buf bytes.Buffer
		buf.WriteString("RIFF")
		_ = binary.Write(&buf, binary.LittleEndian, uint32(36))
		buf.WriteString("WAVEfmt ")
		_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
		_ = binary.Write(&buf, binary.LittleEndian, rate)
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
		buf.WriteString("data")
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))

		if _, err := NewReader(bytes.NewReader(buf.Bytes())); !errors.Is(err, ErrUnsupportedWAV) {
			t.Errorf("rate %d: err = %v, want ErrUnsupportedWAV", rate, err)
		}
	}
}

func wavHeader(riffSize, dataSize uint32, channels uint16) []byte {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, riffSize)
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, channels)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(48000))
	_ = binary.Write(&buf, binary.LittleEndian, 48000*2*uint32(channels))
	_ = binary.Write(&buf, binary.LittleEndian, channels*2)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, dataSize)
	return buf.Bytes()
}

func readAll(t *testing.T, r *Reader, chunk int) []int16 {
	t.Helper()
	var out []int16
	dst := make([]int16, chunk)
	for {
		n, err := r.ReadInt16PCM(dst)
		out = append(out, dst[:n]...)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWAVReader_StreamingSizes(t *testing.T) {
	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0}
	for name, hdr := range map[string][]byte{
		"data 0xFFFFFFFF":         wavHeader(0xFFFFFFFF, 0xFFFFFFFF, 1),
		"riff 0, data 0":          wavHeader(0, 0, 1),
		"riff 0xFFFFFFFF, data 0": wavHeader(0xFFFFFFFF, 0, 1),
	} {
		r, err := NewReader(bytes.NewReader(append(append([]byte(nil), hdr...), pcm...)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := readAll(t, r, 4); len(got) != 6 || got[5] != 6 {
			t.Errorf("%s: got %v, want 6 samples", name, got)
		}
	}
	// A real empty file (valid RIFF size, data 0) stays empty.
	r, err := NewReader(bytes.NewReader(append(wavHeader(36, 0, 1), pcm...)))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r, 4); len(got) != 0 {
		t.Errorf("empty data chunk returned %v", got)
	}
}

func TestWAVReader_WholeFramesOnly(t *testing.T) {
	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0} // 2.5 stereo frames
	r, err := NewReader(bytes.NewReader(append(wavHeader(36+10, 10, 2), pcm...)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadInt16PCM(make([]int16, 1)); !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("err = %v, want io.ErrShortBuffer", err)
	}
	n, err := r.ReadInt16PCM(make([]int16, 3)) // odd dst: one frame only
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v, want 2 samples", n, err)
	}
	got := readAll(t, r, 8)
	if len(got) != 2 || got[0] != 3 || got[1] != 4 { // the trailing half frame is dropped
		t.Fatalf("rest = %v", got)
	}
}

func TestWriterValidatesArgumentsAndKeepsCloseError(t *testing.T) {
	for _, tc := range [][2]int{{0, 2}, {48000, 0}, {-1, 1}, {48000, 40000}, {1 << 31, 2}} {
		if _, err := NewWriter(&memWS{}, tc[0], tc[1]); err == nil {
			t.Errorf("NewWriter(%d, %d) accepted", tc[0], tc[1])
		}
	}
	ws := &memWS{}
	w, err := NewWriter(ws, 48000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteInt16PCM([]int16{1, 2, 3}); err == nil {
		t.Error("partial frame accepted")
	}
	ws.seekErr = errors.New("seek failed")
	if err := w.Close(); err == nil {
		t.Fatal("first Close should fail")
	}
	if err := w.Close(); err == nil {
		t.Fatal("second Close must repeat the error")
	}
}

type memWS struct {
	b       []byte
	pos     int
	seekErr error
}

func (m *memWS) Write(p []byte) (int, error) {
	if end := m.pos + len(p); end > len(m.b) {
		m.b = append(m.b, make([]byte, end-len(m.b))...)
	}
	copy(m.b[m.pos:], p)
	m.pos += len(p)
	return len(p), nil
}

func (m *memWS) Seek(off int64, whence int) (int64, error) {
	if m.seekErr != nil && whence == io.SeekStart {
		return 0, m.seekErr
	}
	switch whence {
	case io.SeekStart:
		m.pos = int(off)
	case io.SeekCurrent:
		m.pos += int(off)
	case io.SeekEnd:
		m.pos = len(m.b) + int(off)
	}
	return int64(m.pos), nil
}

// The patched sizes must land relative to where the header started.
func TestWriterPatchesRelativeToStartOffset(t *testing.T) {
	ws := &memWS{b: []byte("PREFIX!!"), pos: 8}
	w, err := NewWriter(ws, 48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.WriteInt16PCM([]int16{1, 2, 3, 4})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(bytes.NewReader(ws.b[8:]))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r, 16); len(got) != 4 {
		t.Fatalf("got %v", got)
	}
	if string(ws.b[:8]) != "PREFIX!!" {
		t.Fatal("prefix clobbered")
	}
}
