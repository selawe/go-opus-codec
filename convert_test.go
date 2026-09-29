package opusgo

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/selawe/go-opus-codec/ogg"
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

func TestDecodeOggOpusToWAV_OutputLimit(t *testing.T) {
	wavData := generateSineWAV(t, 48000, 1, 1500)
	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &oggBuf, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus: %v", err)
	}

	outWAV := &memWriteSeeker{}
	// 1500 samples * 2 bytes = 3000 bytes. Limit to 1000 bytes.
	err := DecodeOggOpusToWAV(&oggBuf, outWAV, WithMaxOutputBytes(1000))
	if !errors.Is(err, ErrOutputLimitExceeded) {
		t.Fatalf("expected ErrOutputLimitExceeded, got %v", err)
	}
}

func TestConvertFiles_CleanupOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	badSrc := filepath.Join(tmpDir, "bad.wav")
	dstOgg := filepath.Join(tmpDir, "out.ogg")
	if err := os.WriteFile(badSrc, []byte("not a wav file"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ConvertWAVFileToOggOpus(badSrc, dstOgg, nil); err == nil {
		t.Fatal("expected error converting invalid WAV")
	}
	if _, err := os.Stat(dstOgg); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected %s to be cleaned up, but it exists", dstOgg)
	}

	badOgg := filepath.Join(tmpDir, "bad.ogg")
	dstWAV := filepath.Join(tmpDir, "out.wav")
	if err := os.WriteFile(badOgg, []byte("not an ogg file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ConvertOggOpusFileToWAV(badOgg, dstWAV); err == nil {
		t.Fatal("expected error converting invalid Ogg")
	}
	if _, err := os.Stat(dstWAV); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected %s to be cleaned up, but it exists", dstWAV)
	}
}

func TestEncodeWAVToOggOpus_EmptyInputHasEOS(t *testing.T) {
	emptyWAV := generateSineWAV(t, 48000, 1, 0)
	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(emptyWAV), &oggBuf, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus empty: %v", err)
	}

	pr := ogg.NewPageReader(&oggBuf)
	var hasEOS bool
	for {
		p, err := pr.ReadPage()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPage: %v", err)
		}
		if p.IsEOS() {
			hasEOS = true
		}
	}
	if !hasEOS {
		t.Fatal("empty input was encoded without EOS page")
	}
}

// RFC 7845 Section 4: a page's granule position counts every sample decoded so
// far, pre-skip included, so interior pages carry frames*frameSize and only the
// EOS page carries the pre-skip plus the true input length.
func TestEncodeWAVToOggOpus_GranulePositions(t *testing.T) {
	const frameSize = 960 // default 20 ms at 48 kHz
	for _, n := range []int{1900, 2 * frameSize, 43000, 48000} {
		src := generateSineWAV(t, 48000, 1, n)
		var oggBuf bytes.Buffer
		if err := EncodeWAVToOggOpus(bytes.NewReader(src), &oggBuf, nil); err != nil {
			t.Fatalf("n=%d: encode: %v", n, err)
		}
		r, err := ogg.NewOpusReader(&oggBuf)
		if err != nil {
			t.Fatalf("n=%d: NewOpusReader: %v", n, err)
		}
		var (
			count int
			prev  uint64
			last  *ogg.OpusAudioPacket
		)
		for {
			pkt, err := r.ReadAudioPacket()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("n=%d: ReadAudioPacket: %v", n, err)
			}
			count++
			if pkt.GranuleValid {
				if pkt.GranulePos < prev {
					t.Fatalf("n=%d: packet %d: granule %d decreased from %d", n, count, pkt.GranulePos, prev)
				}
				prev = pkt.GranulePos
			}
			last = pkt
		}
		if last == nil || !last.EOS {
			t.Fatalf("n=%d: last packet missing EOS", n)
		}
		if want := uint64(r.Head.PreSkip) + uint64(n); last.GranulePos != want {
			t.Errorf("n=%d: EOS granule = %d, want %d", n, last.GranulePos, want)
		}
	}
}

func TestEncodeWAVToOggOpus_InteriorGranuleExcludesPreSkip(t *testing.T) {
	const frameSize = 960
	src := generateSineWAV(t, 48000, 1, 120*frameSize)
	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(src), &oggBuf, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	r, err := ogg.NewOpusReader(&oggBuf)
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}
	for {
		pkt, err := r.ReadAudioPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadAudioPacket: %v", err)
		}
		if pkt.GranuleValid && !pkt.EOS && pkt.GranulePos%frameSize != 0 {
			t.Errorf("interior granule %d is not a multiple of %d (pre-skip leaked in)", pkt.GranulePos, frameSize)
		}
	}
}

// Framing must batch audio packets (RFC 3533 Section 6) while keeping the
// RFC 7845 layout: OpusHead alone on the BOS page, OpusTags ending its page,
// and audio starting on a fresh page.
func TestEncodeWAVToOggOpus_BatchesPages(t *testing.T) {
	const n = 4 * 48000
	src := generateSineWAV(t, 48000, 1, n)
	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(src), &oggBuf, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw := append([]byte(nil), oggBuf.Bytes()...)

	pr := ogg.NewPageReader(bytes.NewReader(raw))
	var pages []*ogg.Page
	for {
		p, err := pr.ReadPage()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPage: %v", err)
		}
		pages = append(pages, p)
	}
	if len(pages) < 4 {
		t.Fatalf("got %d pages, want at least 4", len(pages))
	}
	if !pages[0].IsBOS() || !bytes.HasPrefix(pages[0].SegmentData, []byte("OpusHead")) || len(pages[0].SegmentTable) > 2 {
		t.Errorf("page 0 is not a lone OpusHead BOS page")
	}
	if !bytes.HasPrefix(pages[1].SegmentData, []byte("OpusTags")) {
		t.Fatalf("page 1 does not start with OpusTags")
	}
	if bytes.HasPrefix(pages[2].SegmentData, []byte("OpusTags")) || pages[2].IsContinuedPacket() {
		t.Errorf("first audio page must start a fresh packet on its own page")
	}
	if !pages[len(pages)-1].IsEOS() {
		t.Errorf("last page is not EOS")
	}

	audioPages := len(pages) - 2
	const packets = n / 960
	if audioPages*4 > packets {
		t.Errorf("%d audio pages for %d packets: packets are not batched", audioPages, packets)
	}
	var prev uint64
	for i, p := range pages[2:] {
		if p.GranulePosition < prev {
			t.Fatalf("audio page %d: granule %d decreased from %d", i, p.GranulePosition, prev)
		}
		prev = p.GranulePosition
		if i < audioPages-1 && len(p.SegmentTable) > 0 {
			if dur := len(p.SegmentTable); dur > 60 {
				t.Errorf("audio page %d holds %d packets (over ~1 s)", i, dur)
			}
		}
	}

	// Batched pages must still round-trip to the exact input length.
	var wavOut memWriteSeeker
	if err := DecodeOggOpusToWAV(bytes.NewReader(raw), &wavOut); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, _, got := readAllWAVSamples(t, wavOut.Bytes()); got != n {
		t.Errorf("decoded %d samples, want %d", got, n)
	}
}

func TestConvertFiles_NeverDestroyExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Refuse to write over the input.
	wavPath := filepath.Join(tmpDir, "in.wav")
	if err := os.WriteFile(wavPath, generateSineWAV(t, 48000, 1, 4800), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(wavPath)
	if err := ConvertWAVFileToOggOpus(wavPath, wavPath, nil); err == nil {
		t.Fatal("expected an error when dst == src")
	}
	if after, _ := os.ReadFile(wavPath); !bytes.Equal(before, after) {
		t.Fatal("input WAV was modified")
	}

	oggPath := filepath.Join(tmpDir, "in.ogg")
	if err := ConvertWAVFileToOggOpus(wavPath, oggPath, nil); err != nil {
		t.Fatal(err)
	}
	before, _ = os.ReadFile(oggPath)
	if err := ConvertOggOpusFileToWAV(oggPath, oggPath); err == nil {
		t.Fatal("expected an error when dst == src")
	}
	if after, _ := os.ReadFile(oggPath); !bytes.Equal(before, after) {
		t.Fatal("input Ogg was modified")
	}

	// A failed conversion keeps a pre-existing destination intact.
	bad := filepath.Join(tmpDir, "bad.wav")
	if err := os.WriteFile(bad, []byte("not a wav file"), 0600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmpDir, "keep.ogg")
	if err := os.WriteFile(dst, []byte("previous good output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ConvertWAVFileToOggOpus(bad, dst, nil); err == nil {
		t.Fatal("expected error")
	}
	if got, _ := os.ReadFile(dst); string(got) != "previous good output" {
		t.Fatalf("existing destination was destroyed: %q", got)
	}
	if ents, _ := os.ReadDir(tmpDir); len(ents) != 4 {
		t.Errorf("unexpected leftover files: %v", ents)
	}

	// A successful conversion replaces it.
	if err := ConvertWAVFileToOggOpus(wavPath, dst, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.HasPrefix(got, []byte("OggS")) {
		t.Fatal("destination was not replaced by the new output")
	}
}
