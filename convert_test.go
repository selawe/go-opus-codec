package opusgo

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/selawe/go-opus-codec/wav"
)

// memWriteSeeker implements io.WriteSeeker in memory for testing.
type memWriteSeeker struct {
	buf []byte
	pos int64
}

func (m *memWriteSeeker) Write(p []byte) (n int, err error) {
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

func (m *memWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = m.pos + offset
	case io.SeekEnd:
		newPos = int64(len(m.buf)) + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if newPos < 0 {
		return 0, errors.New("negative seek position")
	}
	m.pos = newPos
	return newPos, nil
}

func (m *memWriteSeeker) Bytes() []byte {
	return m.buf
}

func generateSineWAV(t *testing.T, sampleRate int, channels int, totalSamplesPerCh int) []byte {
	t.Helper()
	ws := &memWriteSeeker{}
	ww, err := wav.NewWriter(ws, sampleRate, channels)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	totalSamples := totalSamplesPerCh * channels
	pcm := make([]int16, totalSamples)
	freq := 440.0
	for i := 0; i < totalSamplesPerCh; i++ {
		val := int16(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)) * 16000)
		for ch := 0; ch < channels; ch++ {
			pcm[i*channels+ch] = val
		}
	}

	if err := ww.WriteInt16PCM(pcm); err != nil {
		t.Fatalf("WriteInt16PCM: %v", err)
	}
	if err := ww.Close(); err != nil {
		t.Fatalf("wav Close: %v", err)
	}
	return ws.Bytes()
}

func readAllWAVSamples(t *testing.T, data []byte) (channels int, sampleRate int, totalSamplesPerCh int) {
	t.Helper()
	wr, err := wav.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("wav.NewReader: %v", err)
	}
	channels = wr.Channels()
	sampleRate = wr.SampleRate()

	buf := make([]int16, 1024)
	totalInterleaved := 0
	for {
		n, err := wr.ReadInt16PCM(buf)
		if n > 0 {
			totalInterleaved += n
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadInt16PCM: %v", err)
		}
	}
	totalSamplesPerCh = totalInterleaved / channels
	return channels, sampleRate, totalSamplesPerCh
}

func TestEncodeWAVToOggOpus_And_DecodeOggOpusToWAV_Mono(t *testing.T) {
	// Generate 1500 samples (non-frame-aligned at 20ms = 960 samples, tests EOS trimming)
	const sampleCount = 1500
	wavData := generateSineWAV(t, 48000, 1, sampleCount)

	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &oggBuf, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus: %v", err)
	}
	if oggBuf.Len() == 0 {
		t.Fatalf("encoded ogg data is empty")
	}

	outWAV := &memWriteSeeker{}
	if err := DecodeOggOpusToWAV(&oggBuf, outWAV); err != nil {
		t.Fatalf("DecodeOggOpusToWAV: %v", err)
	}

	ch, sr, decodedSamples := readAllWAVSamples(t, outWAV.Bytes())
	if ch != 1 {
		t.Errorf("expected 1 channel, got %d", ch)
	}
	if sr != 48000 {
		t.Errorf("expected 48000 Hz, got %d", sr)
	}
	if decodedSamples != sampleCount {
		t.Errorf("expected %d samples decoded after EOS trimming, got %d", sampleCount, decodedSamples)
	}
}

func TestEncodeWAVToOggOpus_And_DecodeOggOpusToWAV_Stereo_CustomOptions(t *testing.T) {
	const sampleCount = 2880 // exactly 1 frame of 60ms at 48kHz
	wavData := generateSineWAV(t, 48000, 2, sampleCount)

	opts := &EncodeOptions{
		Bitrate:     96000,
		CBR:         true,
		Complexity:  8,
		Application: ApplicationAudio,
		FrameSizeMS: 60,
		Vendor:      "custom-vendor",
		Comments:    []string{"TITLE=Test Stereo", "ARTIST=OpusGo"},
	}

	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &oggBuf, opts); err != nil {
		t.Fatalf("EncodeWAVToOggOpus: %v", err)
	}

	outWAV := &memWriteSeeker{}
	if err := DecodeOggOpusToWAV(&oggBuf, outWAV); err != nil {
		t.Fatalf("DecodeOggOpusToWAV: %v", err)
	}

	ch, sr, decodedSamples := readAllWAVSamples(t, outWAV.Bytes())
	if ch != 2 {
		t.Errorf("expected 2 channels, got %d", ch)
	}
	if sr != 48000 {
		t.Errorf("expected 48000 Hz, got %d", sr)
	}
	if decodedSamples != sampleCount {
		t.Errorf("expected %d samples decoded, got %d", sampleCount, decodedSamples)
	}
}

func TestConvertFileHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	wavInPath := filepath.Join(tmpDir, "input.wav")
	oggPath := filepath.Join(tmpDir, "encoded.opus")
	wavOutPath := filepath.Join(tmpDir, "output.wav")

	const sampleCount = 1920 // 2 frames of 20ms
	originalWAV := generateSineWAV(t, 48000, 1, sampleCount)
	if err := os.WriteFile(wavInPath, originalWAV, 0o644); err != nil {
		t.Fatalf("write input wav: %v", err)
	}

	if err := ConvertWAVFileToOggOpus(wavInPath, oggPath, nil); err != nil {
		t.Fatalf("ConvertWAVFileToOggOpus: %v", err)
	}

	if _, err := os.Stat(oggPath); err != nil {
		t.Fatalf("ogg file was not created: %v", err)
	}

	if err := ConvertOggOpusFileToWAV(oggPath, wavOutPath); err != nil {
		t.Fatalf("ConvertOggOpusFileToWAV: %v", err)
	}

	decodedWAV, err := os.ReadFile(wavOutPath)
	if err != nil {
		t.Fatalf("read decoded wav: %v", err)
	}

	ch, sr, decodedSamples := readAllWAVSamples(t, decodedWAV)
	if ch != 1 || sr != 48000 || decodedSamples != sampleCount {
		t.Errorf("decoded wav mismatch: ch=%d, sr=%d, samples=%d (expected 1, 48000, %d)",
			ch, sr, decodedSamples, sampleCount)
	}
}

func TestEncodeWAV_ValidationErrors(t *testing.T) {
	// Nil checks
	if err := EncodeWAVToOggOpus(nil, &bytes.Buffer{}, nil); err == nil {
		t.Error("expected error for nil wavReader, got nil")
	}
	if err := EncodeWAVToOggOpus(bytes.NewReader(nil), nil, nil); err == nil {
		t.Error("expected error for nil oggWriter, got nil")
	}

	// Unsupported sample rate
	badRateWAV := generateSineWAV(t, 44100, 1, 1000)
	if err := EncodeWAVToOggOpus(bytes.NewReader(badRateWAV), &bytes.Buffer{}, nil); err == nil {
		t.Error("expected error for 44.1kHz WAV, got nil")
	}

	// Unsupported frame duration
	validWAV := generateSineWAV(t, 48000, 1, 1000)
	opts := &EncodeOptions{FrameSizeMS: 25}
	if err := EncodeWAVToOggOpus(bytes.NewReader(validWAV), &bytes.Buffer{}, opts); err == nil {
		t.Error("expected error for 25ms frame duration, got nil")
	}
}

func TestDecodeOggOpus_ValidationErrors(t *testing.T) {
	// Nil checks
	if err := DecodeOggOpusToWAV(nil, &memWriteSeeker{}); err == nil {
		t.Error("expected error for nil oggReader, got nil")
	}
	if err := DecodeOggOpusToWAV(bytes.NewReader(nil), nil); err == nil {
		t.Error("expected error for nil wavWriter, got nil")
	}

	// Corrupted Ogg data
	corruptData := []byte("not an ogg stream at all")
	if err := DecodeOggOpusToWAV(bytes.NewReader(corruptData), &memWriteSeeker{}); err == nil {
		t.Error("expected error for corrupt ogg stream, got nil")
	}
}
